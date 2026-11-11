"""Validation branch only: capture the built Machines page on an owned stock simulator."""
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
from urllib.parse import parse_qs, quote, urlsplit


ROOT = Path(__file__).resolve().parents[2]
EVIDENCE = ROOT / '.ci-evidence/machines'


def serve():
    spec = importlib.util.spec_from_file_location('metrics_mock', ROOT / 'scripts/mock/mock_hub.py')
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    original = module.H.do_GET

    def get(handler):
        path = urlsplit(handler.path)
        if path.path == '/api/v1/metrics':
            manager = handler.headers.get('X-Persona-Manager') or parse_qs(path.query).get('manager', [None])[0]
            with (EVIDENCE / 'metric-requests.jsonl').open('a') as stream:
                stream.write(json.dumps({'manager': manager}) + '\n')
        return original(handler)

    module.H.do_GET = get
    server = module.ThreadingHTTPServer(('127.0.0.1', 0), module.H)
    server.daemon_threads = True
    module.PORT = server.server_port
    module.ENDPOINTS.append(module.endpoint('127.0.0.1', module.PORT, 'http', 'lan'))
    (EVIDENCE / 'mock-port.txt').write_text(str(module.PORT))
    try:
        server.serve_forever()
    finally:
        server.server_close()


def run(*args, timeout=120, check=True, env=None):
    result = subprocess.run(args, text=True, capture_output=True, timeout=timeout, env=env)
    with (EVIDENCE / 'commands.log').open('a') as stream:
        stream.write(' '.join(args) + '\n' + result.stdout + result.stderr + '\n')
    if check and result.returncode:
        raise RuntimeError(f'{args[0]} exited {result.returncode}: {result.stderr[-1000:]}')
    return result.stdout


def capture(log):
    EVIDENCE.mkdir(parents=True, exist_ok=True)
    match = re.search(r'apple-ci: PASS \(exit 0\); evidence: (.+)', Path(log).read_text())
    if not match:
        raise RuntimeError('The complete native lane has not passed')
    output = Path(match[1].strip())
    app = output / 'ios-tests/Build/Products/Debug-iphonesimulator/Persona.app'
    if not app.is_dir():
        raise RuntimeError('Missing actual native test-host app')
    env = dict(os.environ, PERSONA_CONFIG=str(output / 'config.json'))
    config = json.loads((output / 'config.json').read_text())
    bundle = config['app']['bundle_id']
    runtimes = run('xcrun', 'simctl', 'list', 'runtimes', '-j')
    runtime_file = EVIDENCE / 'runtimes.json'
    runtime_file.write_text(runtimes)
    runtime = run('python3', str(ROOT / 'scripts/ci/apple_runtime.py'), str(runtime_file), '--platform', 'iOS').strip()
    name = 'Persona Metrics ' + os.environ['GITHUB_RUN_ID']
    mock_log = (EVIDENCE / 'mock.log').open('w')
    mock = subprocess.Popen([sys.executable, __file__, '--serve'], stdout=mock_log, stderr=subprocess.STDOUT,
                            env=dict(env, PORT='0'), start_new_session=True)
    sim = None
    try:
        deadline = time.monotonic() + 45
        while not (EVIDENCE / 'mock-port.txt').exists():
            if mock.poll() is not None or time.monotonic() >= deadline:
                raise RuntimeError('Mock hub did not become ready')
            time.sleep(0.2)
        port = (EVIDENCE / 'mock-port.txt').read_text()
        created = run('bash', str(ROOT / 'scripts/app/sim-boot.sh'), '--create-only', '--profile', 'stock',
                      '--runtime', runtime, '--device-type', 'iPhone 17 Pro', name, env=env)
        sim = re.search(r'(?m)^[0-9A-Fa-f-]{36}$', created).group(0)
        run('bash', str(ROOT / 'scripts/app/sim-boot.sh'), '--profile', 'stock', sim, timeout=450, env=env)
        run('xcrun', 'simctl', 'install', sim, str(app))
        pair = 'persona://pair?url=' + quote('http://127.0.0.1:' + port, safe='') + '&token=mock'
        for size in ['large', 'accessibility-extra-extra-extra-large']:
            run('xcrun', 'simctl', 'ui', sim, 'content_size', size)
            for appearance in ['light', 'dark']:
                run('xcrun', 'simctl', 'ui', sim, 'appearance', appearance)
                run('xcrun', 'simctl', 'terminate', sim, bundle, check=False)
                run('xcrun', 'simctl', 'launch', sim, bundle, '-pairURL', pair, '-tab', 'assistant',
                    '-openShellPage', 'machines', '-ui.style', 'glass', '-AppleLanguages', '(zh-Hans)',
                    '-AppleLocale', 'zh_CN')
                time.sleep(12)
                run('xcrun', 'simctl', 'io', sim, 'screenshot', str(EVIDENCE / f'machines-{size}-{appearance}.png'))
        requests = [json.loads(line) for line in (EVIDENCE / 'metric-requests.jsonl').read_text().splitlines()]
        managers = {request['manager'] for request in requests}
        if not {'assistant', 'm-3fa21c'}.issubset(managers) or None in managers:
            raise RuntimeError(f'Actual app did not issue the required scoped requests: {managers}')
        print('Machines screenshots captured; actual app requested both online managers without an unscoped duplicate.')
    finally:
        try:
            if sim:
                run('xcrun', 'simctl', 'shutdown', sim, check=False)
                run('xcrun', 'simctl', 'delete', sim, check=False)
            else:
                devices = json.loads(run('xcrun', 'simctl', 'list', 'devices', '-j'))
                for entries in devices['devices'].values():
                    for device in entries:
                        if device['name'] == name:
                            run('xcrun', 'simctl', 'shutdown', device['udid'], check=False)
                            run('xcrun', 'simctl', 'delete', device['udid'], check=False)
        finally:
            if mock.poll() is None:
                os.killpg(mock.pid, signal.SIGTERM)
                try:
                    mock.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    os.killpg(mock.pid, signal.SIGKILL)
                    mock.wait()
            mock_log.close()


if __name__ == '__main__':
    if sys.argv[1] == '--serve':
        serve()
    else:
        capture(sys.argv[1])
