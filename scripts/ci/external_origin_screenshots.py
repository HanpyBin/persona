"""Validation-only emulator evidence for #4203; not part of the product PR."""
import argparse
import json
import os
import re
import signal
import subprocess
import sys
import time
import xml.etree.ElementTree as ET
from pathlib import Path


APP = "com.example.persona.shots"
FROZEN = "2026-11-02T04:00:00+00:00"
CASES = (
    ("lark_agent", "External Lark fixture", "Summarize the release discussion.", "From Lark /agent"),
    ("extension", "External extension fixture", "Draft the weekly notes.", "From an extension"),
    ("user", "Own conversation fixture", "Write a short note.", None),
)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--serial", required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    adb_path = Path(os.environ["ANDROID_HOME"]) / "platform-tools/adb"

    def adb(*values, timeout=30, binary=False):
        return subprocess.run([str(adb_path), "-s", args.serial, *map(str, values)], check=True,
                              capture_output=True, text=not binary, timeout=timeout).stdout

    assert args.serial.startswith("emulator-")
    assert adb("shell", "getprop", "ro.kernel.qemu").strip() == "1"
    adb("shell", "pm", "clear", APP)
    adb("shell", "pm", "grant", APP, "android.permission.POST_NOTIFICATIONS")
    adb("shell", "cmd", "locale", "set-app-locales", APP, "--user", "0", "--locales", "en")
    previous_font = adb("shell", "settings", "get", "system", "font_scale").strip()
    previous_animations = {key: adb("shell", "settings", "get", "global", key).strip() for key in
                           ("animator_duration_scale", "transition_animation_scale", "window_animation_scale")}
    for key in previous_animations:
        adb("shell", "settings", "put", "global", key, "0")

    driver = Path("scripts/app/android.sh").read_text()
    profile = driver.split("<<'MOCK_PY' &\n", 1)[1].split("\nMOCK_PY\n", 1)[0]
    insertion = '''
for index, (origin, title, request) in enumerate([
    ("lark_agent", "External Lark fixture", "Summarize the release discussion."),
    ("extension", "External extension fixture", "Draft the weekly notes."),
    ("user", "Own conversation fixture", "Write a short note."),
]):
    sid = "f00d0001-0000-4000-8000-%012d" % (index + 1)
    session = namespace["assistant_sess"](sid, title, 5, "Ready.")
    session.update(origin=origin, status="idle", state="idle")
    namespace["ASSISTANT"].append(session)
    namespace["ITEMS"][sid] = [
        namespace["it"]("user", "text", 5, text=request, seq=1),
        namespace["it"]("assistant", "text", 4, text="Ready.", seq=2),
        namespace["it"]("user", "text", 3, text="Continue.", seq=3),
        namespace["it"]("assistant", "text", 2, text="Finished.", seq=4),
    ]
'''
    profile = profile.replace("# Derive anonymous labels", insertion + "\n# Derive anonymous labels", 1)
    log = (args.out / "mock-hub.log").open("w")
    server = subprocess.Popen([sys.executable, "-u", "-c", profile], stdout=log, stderr=subprocess.STDOUT,
                              env={**os.environ, "PORT": "0", "MOCK_FROZEN": FROZEN,
                                   "MOCK_ORIGINS": "0", "MOCK_AUTO_CONVERSATIONS": "0", "MOCK_TASKS": "1"})
    port = None
    evidence = []

    def dump():
        adb("shell", "uiautomator", "dump", "--compressed", "/sdcard/external-origin.xml", timeout=45)
        return ET.fromstring(adb("shell", "cat", "/sdcard/external-origin.xml"))

    def wait_for(predicate, description):
        deadline = time.monotonic() + 75
        while time.monotonic() < deadline:
            root = dump()
            if any(node.get("package") == "android" and "isn't responding" in node.get("text", "")
                   for node in root.iter("node")):
                capture("failure-ui", root)
                (args.out / "failure-logcat.log").write_text(adb("logcat", "-d", "-t", "500"))
                raise AssertionError("The owned emulator is showing an Android ANR dialog")
            if predicate(root) and any(node.get("package") == APP for node in root.iter("node")):
                return root
        capture("failure-ui", root)
        (args.out / "failure-logcat.log").write_text(adb("logcat", "-d", "-t", "500"))
        raise AssertionError("UI did not reach " + description)

    def has_text(root, text):
        return any(text in node.get("text", "") for node in root.iter("node"))

    def descriptions(root, text):
        return [node for node in root.iter("node") if text in node.get("content-desc", "")]

    def click(predicate, description):
        root = wait_for(lambda tree: any(predicate(node) for node in tree.iter("node")), description)
        matches = [node for node in root.iter("node") if predicate(node)]
        assert len(matches) == 1, (description, len(matches))
        bounds = re.fullmatch(r"\[(\d+),(\d+)\]\[(\d+),(\d+)\]", matches[0].get("bounds", ""))
        assert bounds, description
        left, top, right, bottom = map(int, bounds.groups())
        assert right > left and bottom > top, description
        adb("shell", "input", "tap", (left + right) // 2, (top + bottom) // 2)

    def capture(name, root):
        (args.out / (name + ".xml")).write_bytes(ET.tostring(root, encoding="utf-8"))
        (args.out / (name + ".png")).write_bytes(adb("exec-out", "screencap", "-p", binary=True))

    def launch(style, theme, session):
        adb("shell", "am", "force-stop", APP)
        sid = "f00d0001-0000-4000-8000-%012d" % (session + 1)
        command = ["shell", "am", "start", "-W", "-n", APP + "/persona.android.debug.DebugEntryActivity",
                   "--es", "debug.mockAddress", "http://127.0.0.1:" + str(port),
                   "--es", "debug.automation", "true", "--es", "debug.style", style,
                   "--es", "debug.theme", theme, "--es", "debug.openSession", machine + "/" + sid,
                   "--es", "debug.conversationKind", "assistant"]
        adb(*command)

    try:
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            assert server.poll() is None, "Owned mock hub exited"
            match = re.search(r"mock hub on 127\.0\.0\.1:(\d+)", (args.out / "mock-hub.log").read_text())
            if match:
                port = int(match[1])
                break
            time.sleep(0.1)
        assert port is not None, "Owned mock hub did not become ready"
        import urllib.request
        with urllib.request.urlopen("http://127.0.0.1:%d/api/v1/assistant" % port, timeout=10) as response:
            configuration = json.load(response)
        rows = configuration["conversations"]
        machine = next(row["machine_id"] for row in rows if row["session_id"] == "f00d0001-0000-4000-8000-000000000001")
        adb("reverse", "tcp:" + str(port), "tcp:" + str(port))
        for scale in (1.0, 2.0):
            adb("shell", "settings", "put", "system", "font_scale", str(scale))
            actual_scale = float(adb("shell", "settings", "get", "system", "font_scale").strip())
            assert actual_scale == scale
            for style in ("glass", "brief", "board"):
                for theme in ("light", "dark"):
                    adb("shell", "cmd", "uimode", "night", "yes" if theme == "dark" else "no")
                    for index, (origin, title, request, source) in enumerate(CASES):
                        launch(style, theme, index)
                        root = wait_for(lambda tree: has_text(tree, request) and has_text(tree, "Finished."), request)
                        for caption in ("From Lark /agent", "From an extension"):
                            assert len(descriptions(root, caption)) == int(caption == source), (origin, caption)
                        name = "%s-%s-font%s-chat-%s" % (style, theme, scale, origin)
                        capture(name, root)
                        evidence.append({"name": name, "fontScale": actual_scale, "origin": origin,
                                         "sourceLineCount": int(source is not None)})
                    launch(style, theme, 0)
                    wait_for(lambda tree: has_text(tree, CASES[0][2]), "Lark conversation before opening sidebar")
                    click(lambda node: node.get("content-desc") == "Sidebar", "Sidebar")
                    click(lambda node: node.get("text", "").startswith("Automatic"), "Automatic chats")
                    for chip, title, source in (("Lark /agent", CASES[0][1], CASES[0][3]),
                                                ("Extensions", CASES[1][1], CASES[1][3])):
                        click(lambda node: node.get("text", "").split("\n")[0] == chip, chip)
                        root = wait_for(lambda tree: has_text(tree, title), title)
                        assert len(descriptions(root, source)) == 1, source
                        name = "%s-%s-font%s-drawer-%s" % (style, theme, scale, "lark" if chip == "Lark /agent" else "extension")
                        capture(name, root)
                        evidence.append({"name": name, "fontScale": actual_scale, "rowSourceMarks": 1})
        (args.out / "evidence.json").write_text(json.dumps({"passed": True, "cases": evidence}, indent=2) + "\n")
        print("External-origin native emulator evidence: PASS (%d captures)" % len(evidence))
    finally:
        try:
            adb("shell", "am", "force-stop", APP)
            if port is not None:
                adb("reverse", "--remove", "tcp:" + str(port))
            settings = [("system", "font_scale", previous_font)] + [
                ("global", key, value) for key, value in previous_animations.items()]
            for namespace, key, value in settings:
                if value == "null":
                    adb("shell", "settings", "delete", namespace, key)
                else:
                    adb("shell", "settings", "put", namespace, key, value)
        finally:
            server.terminate()
            try:
                server.wait(timeout=10)
            except subprocess.TimeoutExpired:
                server.kill()
                server.wait(timeout=5)
            log.close()


if __name__ == "__main__":
    def stop(signum, frame):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, stop)
    main()
