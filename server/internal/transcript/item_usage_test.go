package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// claudeReplyLine is one content block of a reply as Claude Code writes it: a line of its own, repeating the request's
// usage (usage is the JSON object, "" for none).
func claudeReplyLine(uuid, msg, model, block, usage string, extra ...string) string {
	u := ""
	if usage != "" {
		u = `,"usage":` + usage
	}
	return `{"type":"assistant","uuid":"` + uuid + `","timestamp":"2026-11-12T08:00:00Z"` + strings.Join(extra, "") +
		`,"message":{"role":"assistant","model":"` + model + `","id":"` + msg + `","content":[` + block + `]` + u + `}}`
}

func claudeToolResultLine(uuid, call string) string {
	return `{"type":"user","uuid":"` + uuid + `","timestamp":"2026-11-12T08:00:00Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"` + call + `","content":"ok"}]}}`
}

const (
	thinkingBlock = `{"type":"thinking","thinking":"Thinking it over."}`
	textBlock     = `{"type":"text","text":"Here it is."}`
)

func toolBlock(id string) string {
	return `{"type":"tool_use","id":"` + id + `","name":"Bash","input":{"command":"ls"}}`
}

// usageOf maps each item that carries usage to it.
func usageOf(items []proto.Item) map[string][]proto.TokenUsage {
	out := map[string][]proto.TokenUsage{}
	for _, it := range items {
		if it.Usage != nil {
			out[it.ID] = it.Usage
		}
	}
	return out
}

// A request's usage rides once, on the first item its lines made: every content block of the reply repeats it (a tool's
// result may come between them). A line that made no item (a thinking block with nothing shown) leaves it to the next
// one. Claude Code's own lines for a failed request, a gateway's zeros and a subagent's sidechain lines carry none; a
// line without a message id is a request of its own. The fast speed and the 1-hour cache writes are kept.
func TestClaudeRequestUsageOnce(t *testing.T) {
	const opus = "claude-opus-5-5"
	u1 := `{"input_tokens":3,"cache_read_input_tokens":40000,"cache_creation_input_tokens":1200,"output_tokens":90,"cache_creation":{"ephemeral_1h_input_tokens":1000,"ephemeral_5m_input_tokens":200},"speed":"fast"}`
	u2 := `{"input_tokens":5,"cache_read_input_tokens":41200,"cache_creation_input_tokens":0,"output_tokens":12,"speed":"standard"}`
	lines := []string{
		`{"type":"user","uuid":"p1","timestamp":"2026-11-12T08:00:00Z","message":{"role":"user","content":"look at it"}}`,
		claudeReplyLine("a1", "msg_1", opus, thinkingBlock, u1),
		claudeReplyLine("a2", "msg_1", opus, textBlock, u1),
		claudeReplyLine("a3", "msg_1", opus, toolBlock("toolu_1"), u1),
		claudeToolResultLine("r1", "toolu_1"),
		claudeReplyLine("a4", "msg_1", opus, toolBlock("toolu_2"), u1), // after a result, still msg_1's
		claudeToolResultLine("r2", "toolu_2"),
		claudeReplyLine("a5", "msg_2", opus, `{"type":"thinking","thinking":"","signature":"x"}`, u2), // no item
		claudeReplyLine("a6", "msg_2", opus, textBlock, u2),
		claudeReplyLine("a7", "msg_3", "<synthetic>", `{"type":"text","text":"API Error: 500"}`, `{"input_tokens":0,"output_tokens":0}`),
		claudeReplyLine("a8", "msg_4", opus, `{"type":"text","text":"Rate limited."}`, `{"input_tokens":7,"output_tokens":1}`, `,"isApiErrorMessage":true,"error":"rate_limit"`),
		claudeReplyLine("a9", "msg_5", "glm-5.3", textBlock, `{"input_tokens":0,"output_tokens":0}`), // a gateway's zeros
		claudeReplyLine("s1", "msg_6", opus, textBlock, u2, `,"isSidechain":true`),
		claudeReplyLine("n1", "", "glm-5.3", textBlock, `{"input_tokens":100,"output_tokens":10}`),
		claudeReplyLine("n2", "", "glm-5.3", textBlock, `{"input_tokens":100,"output_tokens":10}`),
	}
	var m Meta
	var items []proto.Item
	for i, line := range lines {
		items = append(items, ParseLine(i+1, []byte(line), &m)...)
	}
	want := map[string][]proto.TokenUsage{
		"a1#0": {{Model: opus, Input: 3, Output: 90, CacheRead: 40000, CacheWrite: 1200, CacheWrite1h: 1000, Fast: true}},
		"a6#0": {{Model: opus, Input: 5, Output: 12, CacheRead: 41200}},
		"n1#0": {{Model: "glm-5.3", Input: 100, Output: 10}},
		"n2#0": {{Model: "glm-5.3", Input: 100, Output: 10}},
	}
	if got := usageOf(items); !reflect.DeepEqual(got, want) {
		t.Fatalf("usage on %v,\nwant %v", got, want)
	}
	if b, _ := json.Marshal(want["a1#0"]); string(b) != `[{"model":"claude-opus-5-5","in":3,"out":90,"cache_read":40000,"cache_write":1200,"cache_write_1h":1000,"fast":true}]` {
		t.Fatalf("wire: %s", b)
	}
}

// Claude Code up to 2.1.306 wrote the output generated so far on a reply's earlier blocks and more on each later one:
// a later line adds what grew, so a request's items add up to its last usage. A gateway that first reported nothing
// (zeros, then the real usage) is counted once its usage comes.
func TestClaudeRequestUsageGrowsAcrossLines(t *testing.T) {
	lines := []string{
		claudeReplyLine("a1", "msg_1", "claude-opus-5", thinkingBlock, `{"input_tokens":2,"cache_read_input_tokens":500,"output_tokens":4}`),
		claudeReplyLine("a2", "msg_1", "claude-opus-5", textBlock, `{"input_tokens":2,"cache_read_input_tokens":500,"output_tokens":350}`),
		claudeReplyLine("a3", "msg_1", "claude-opus-5", toolBlock("toolu_1"), `{"input_tokens":2,"cache_read_input_tokens":500,"output_tokens":350}`),
		claudeReplyLine("g1", "resp_1", "gpt-6.1-terra", thinkingBlock, `{"input_tokens":0,"output_tokens":0}`),
		claudeReplyLine("g2", "resp_1", "gpt-6.1-terra", textBlock, `{"input_tokens":800,"cache_read_input_tokens":3000,"output_tokens":60}`),
	}
	var m Meta
	var items []proto.Item
	for i, line := range lines {
		items = append(items, ParseLine(i+1, []byte(line), &m)...)
	}
	want := map[string][]proto.TokenUsage{
		"a1#0": {{Model: "claude-opus-5", Input: 2, Output: 4, CacheRead: 500}},
		"a2#0": {{Model: "claude-opus-5", Output: 346}},
		"g2#0": {{Model: "gpt-6.1-terra", Input: 800, Output: 60, CacheRead: 3000}},
	}
	if got := usageOf(items); !reflect.DeepEqual(got, want) {
		t.Fatalf("usage on %v,\nwant %v", got, want)
	}
}

// claudeSession is a transcript of n prompts, each answered by a few requests whose replies are several lines (with a
// tool's result between them, and now and then a first line that shows nothing).
func claudeSession(n int) []string {
	var lines []string
	for p := 0; p < n; p++ {
		lines = append(lines, fmt.Sprintf(`{"type":"user","uuid":"p%d","timestamp":"2026-11-12T08:00:00Z","message":{"role":"user","content":"prompt %d %s"}}`, p, p, strings.Repeat("x", 200)))
		for r := 0; r < 3; r++ {
			msg := fmt.Sprintf("msg_%d_%d", p, r)
			usage := func(out int) string {
				return fmt.Sprintf(`{"input_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d,"output_tokens":%d}`, 1+r, 1000*p+r, 10*r, out)
			}
			first := thinkingBlock
			if (p+r)%4 == 0 {
				first = `{"type":"thinking","thinking":"","signature":"x"}`
			}
			call := fmt.Sprintf("toolu_%d_%d", p, r)
			lines = append(lines,
				claudeReplyLine(msg+"a", msg, "claude-opus-5-5", first, usage(5)),
				claudeReplyLine(msg+"b", msg, "claude-opus-5-5", textBlock, usage(40+p)),
				claudeReplyLine(msg+"c", msg, "claude-opus-5-5", toolBlock(call), usage(40+p)),
				claudeToolResultLine(msg+"r", call),
				claudeReplyLine(msg+"d", msg, "claude-opus-5-5", toolBlock(call+"x"), usage(41+p)),
				claudeToolResultLine(msg+"s", call+"x"),
			)
		}
	}
	return lines
}

// The usage items carry is the same whichever way the lines are read: all at once, a few at a time as a watched session
// gets them, or opened by the file's end, whose windows may start among a reply's lines (window.go seedUsage).
func TestClaudeRequestUsageSameEveryRead(t *testing.T) {
	defer func(w int64) { windowBytes = w }(windowBytes)
	lines := claudeSession(40)
	data := []byte(strings.Join(lines, "\n") + "\n")
	dir := t.TempDir()
	p := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	windowBytes = 1 << 40
	full := NewFile(p)
	if _, err := full.Refresh(); err != nil {
		t.Fatal(err)
	}
	all, _, _ := full.Page(0, 1<<30)
	var sum []proto.TokenUsage
	for _, it := range all {
		for _, u := range it.Usage {
			sum = AddUsage(sum, u)
		}
	}
	// 40 prompts × 3 requests, each counted once at its last usage.
	var want proto.TokenUsage
	for p := 0; p < 40; p++ {
		for r := 0; r < 3; r++ {
			want.Input += int64(1 + r)
			want.CacheRead += int64(1000*p + r)
			want.CacheWrite += int64(10 * r)
			want.Output += int64(41 + p)
		}
	}
	want.Model = "claude-opus-5-5"
	if len(sum) != 1 || sum[0] != want {
		t.Fatalf("a full read sums %+v, want %+v", sum, want)
	}

	// Pushed: the same lines appended a few at a time.
	pushed := filepath.Join(dir, "pushed.jsonl")
	live := NewFile(pushed)
	var got []proto.Item
	for i := 0; i < len(lines); i += 5 {
		fh, err := os.OpenFile(pushed, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fh.WriteString(strings.Join(lines[i:min(i+5, len(lines))], "\n") + "\n")
		fh.Close()
		fresh, err := live.Refresh()
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, fresh...)
	}
	if !reflect.DeepEqual(usageOf(got), usageOf(all)) {
		t.Fatal("pushed lines carry other usage than a full read")
	}

	// Opened by the end, with windows of several sizes so that they start at lines of every kind.
	for _, div := range []int64{7, 9, 13, 23} {
		windowBytes = int64(len(data)) / div
		f := NewFile(p)
		if _, err := f.Refresh(); err != nil {
			t.Fatal(err)
		}
		if f.base == 0 {
			t.Fatal("the file was not opened by its end: the test checks nothing")
		}
		var paged []proto.Item
		for before := 0; ; {
			items, more, _ := f.PageTurns(before, 3, 2000)
			paged = append(append([]proto.Item(nil), items...), paged...)
			if !more || len(items) == 0 {
				break
			}
			before = items[0].Seq
		}
		if !reflect.DeepEqual(usageOf(paged), usageOf(all)) {
			t.Fatalf("windows of 1/%d: other usage than a full read", div)
		}
		if !bytes.Equal(marshal(t, paged), marshal(t, all)) {
			t.Fatalf("windows of 1/%d: other items than a full read", div)
		}
	}
}

// A window seed never reads a huge line just before it (a request blob repeats the whole conversation): it looks past
// it to the reply lines before.
func TestClaudeUsageSeedSkipsHugeLines(t *testing.T) {
	u := `{"input_tokens":2,"output_tokens":9}`
	lines := []string{
		claudeReplyLine("a1", "msg_1", "claude-opus-5-5", textBlock, u),
		`{"type":"api-request-blob","content":"` + strings.Repeat("x", seedUsageLine) + `"}`,
	}
	var starts []int64
	var data []byte
	for _, l := range lines {
		starts = append(starts, int64(len(data)))
		data = append(data, l+"\n"...)
	}
	var read []int
	readLine := func(n int) []byte {
		read = append(read, n)
		end := int64(len(data))
		if n < len(starts) {
			end = starts[n]
		}
		return bytes.TrimRight(data[starts[n-1]:end], "\n")
	}
	size := func(n int) int64 {
		if n < len(starts) {
			return starts[n] - starts[n-1]
		}
		return int64(len(data)) - starts[n-1]
	}
	req, _ := (&File{}).seedUsage(readLine, size, 3, false)
	if req.id != "msg_1" || req.got.Output != 9 {
		t.Fatalf("seed %+v", req)
	}
	if !reflect.DeepEqual(read, []int{1}) {
		t.Fatalf("lines read: %v, want only the reply's", read)
	}
}

// A Codex turn's usage is the sum of its requests, on the turn_duration item its task_complete makes: a token_count
// that repeats the running total (a rate-limit update) is no new request, and the next turn starts from nothing. An
// interrupted turn's goes on its notice.
func TestCodexTurnUsage(t *testing.T) {
	const ts = `"timestamp":"2026-11-12T08:00:00.000Z"`
	ev := func(payload string) string { return `{` + ts + `,"type":"event_msg","payload":` + payload + `}` }
	tokens := func(total, in, cached, out int) string {
		return ev(fmt.Sprintf(`{"type":"token_count","info":{"total_token_usage":{"input_tokens":%d,"output_tokens":1,"total_tokens":%d},"last_token_usage":{"input_tokens":%d,"cached_input_tokens":%d,"output_tokens":%d,"total_tokens":%d}},"rate_limits":null}`, total, total, in, cached, out, in+out))
	}
	item := func(id string) string {
		return ev(`{"type":"item_completed","item":{"type":"AgentMessage","id":"` + id + `","content":[{"type":"Text","text":"done"}]}}`)
	}
	lines := []string{
		`{` + ts + `,"type":"session_meta","payload":{"id":"01a1","cwd":"/r/fake","cli_version":"0.170.0"}}`,
		ev(`{"type":"task_started","turn_id":"t0"}`),
		`{` + ts + `,"type":"turn_context","payload":{"turn_id":"t0","model":"gpt-6.1"}}`,
		tokens(1000, 900, 600, 100),
		tokens(1000, 900, 600, 100), // the same total again: a rate-limit update
		item("m0"),
		tokens(2500, 1300, 1200, 200),
		ev(`{"type":"task_complete","turn_id":"t0","duration_ms":4000}`),
		ev(`{"type":"task_started","turn_id":"t1"}`),
		tokens(2500, 1300, 1200, 200), // a repeat of the last turn's last
		`{` + ts + `,"type":"turn_context","payload":{"turn_id":"t1","model":"gpt-6.1-sol"}}`,
		tokens(3000, 400, 500, 100), // cached above the input: none left uncached
		ev(`{"type":"task_complete","turn_id":"t1","duration_ms":1000}`),
		ev(`{"type":"task_started","turn_id":"t2"}`),
		tokens(3100, 80, 0, 20),
		ev(`{"type":"turn_aborted","turn_id":"t2","reason":"interrupted"}`),
		ev(`{"type":"task_started","turn_id":"t3"}`),
		ev(`{"type":"task_complete","turn_id":"t3","duration_ms":10}`),
	}
	m := Meta{Codex: true}
	var items []proto.Item
	for i, line := range lines {
		items = append(items, ParseLine(i+1, []byte(line), &m)...)
	}
	var got []string
	for _, it := range items {
		if it.Usage != nil {
			got = append(got, fmt.Sprintf("%s:%s:%+v", it.Kind, it.Text, it.Usage))
		}
	}
	want := []string{
		"turn_duration::[{ID: Model:gpt-6.1 Input:400 Output:300 CacheRead:1800 CacheWrite:0 CacheWrite1h:0 Fast:false}]",
		"turn_duration::[{ID: Model:gpt-6.1-sol Input:0 Output:100 CacheRead:500 CacheWrite:0 CacheWrite1h:0 Fast:false}]",
		"notice:" + InterruptedNotice + ":[{ID: Model: Input:80 Output:20 CacheRead:0 CacheWrite:0 CacheWrite1h:0 Fast:false}]",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// codexUsageTurns is codexTurns with the token counts a rollout writes: two requests a turn, and a rate-limit update
// that repeats the running total, at the turn's start.
func codexUsageTurns(n int) []string {
	var out []string
	total := 0
	for _, line := range codexTurns(n) {
		if strings.Contains(line, `"type":"task_started"`) {
			out = append(out, line, fmt.Sprintf(`{"timestamp":"2026-11-10T08:00:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":%d},"last_token_usage":{"input_tokens":500,"cached_input_tokens":100,"output_tokens":50}}}}`, total))
			continue
		}
		if strings.Contains(line, `"type":"task_complete"`) {
			for i := 0; i < 2; i++ {
				total += 550
				out = append(out, fmt.Sprintf(`{"timestamp":"2026-11-10T08:00:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":%d},"last_token_usage":{"input_tokens":500,"cached_input_tokens":100,"output_tokens":50}}}}`, total))
			}
			line = strings.Replace(line, `"type":"task_complete",`, `"type":"task_complete","duration_ms":1000,`, 1)
		}
		out = append(out, line)
	}
	return out
}

// A rollout opened by its end gives the turns the usage a full read does: its windows start at a turn, after a
// token_count that only repeats the total before it.
func TestCodexTurnUsageFullEqualsWindowed(t *testing.T) {
	defer func(w int64) { windowBytes = w }(windowBytes)
	data := []byte(strings.Join(codexUsageTurns(60), "\n") + "\n")
	p := filepath.Join(t.TempDir(), "rollout-2026-11-10T08-00-00-01a1.jsonl")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	windowBytes = 1 << 40
	full := NewFile(p)
	if _, err := full.Refresh(); err != nil {
		t.Fatal(err)
	}
	all, _, _ := full.Page(0, 1<<30)
	carried := 0
	for _, it := range all {
		if it.Usage != nil {
			carried++
			if u := it.Usage[0]; u.Input != 800 || u.CacheRead != 200 || u.Output != 100 {
				t.Fatalf("turn usage %+v", u)
			}
		}
	}
	if carried != 60 {
		t.Fatalf("%d turns carry usage, want 60", carried)
	}
	windowBytes = int64(len(data)) / 8
	f := NewFile(p)
	if _, err := f.Refresh(); err != nil {
		t.Fatal(err)
	}
	if f.base == 0 {
		t.Fatal("the file was not opened by its end: the test checks nothing")
	}
	var got []proto.Item
	for before := 0; ; {
		items, more, _ := f.Page(before, 7)
		got = append(append([]proto.Item(nil), items...), got...)
		if !more || len(items) == 0 {
			break
		}
		before = items[0].Seq
	}
	if !bytes.Equal(marshal(t, got), marshal(t, all)) {
		t.Fatalf("a windowed read differs from a full read (%d and %d items)", len(got), len(all))
	}
}

// A fold counts what its items used: the requests summed per model and speed, a subagent's report (an id) as it is.
func TestFoldSumsUsage(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(seq int, kind, call string, usage ...proto.TokenUsage) proto.Item {
		role := "assistant"
		if kind == proto.KindToolResult {
			role = "user"
		}
		return proto.Item{ID: fmt.Sprintf("l%d#0", seq), Seq: seq, TS: ts, Role: role, Kind: kind, ToolUseID: call, Text: "x", Usage: usage}
	}
	opus := proto.TokenUsage{Model: "claude-opus-5-5", Input: 2, Output: 10, CacheRead: 100, CacheWrite: 50, CacheWrite1h: 50}
	fast := proto.TokenUsage{Model: "claude-opus-5-5", Input: 1, Output: 5, Fast: true}
	glm := proto.TokenUsage{Model: "glm-5.3", Input: 30, Output: 3}
	report := proto.TokenUsage{ID: "agent:a1", Model: "claude-opus-5-5", Input: 7, Output: 70}
	page := []proto.Item{
		mk(1, proto.KindThinking, "", opus),
		mk(2, proto.KindToolUse, "a"), mk(3, proto.KindToolResult, "a", report),
		mk(4, proto.KindThinking, "", opus),
		mk(5, proto.KindToolUse, "b", glm), mk(6, proto.KindToolResult, "b"),
		mk(7, proto.KindThinking, "", fast),
		mk(8, proto.KindToolUse, "c", opus), mk(9, proto.KindToolResult, "c", report),
	}
	out := FoldWork(page)
	if len(out) != 1 || out[0].Kind != proto.KindFold {
		t.Fatalf("not one fold: %+v", out)
	}
	want := []proto.TokenUsage{
		{Model: "claude-opus-5-5", Input: 6, Output: 30, CacheRead: 300, CacheWrite: 150, CacheWrite1h: 150},
		report, glm, fast, report,
	}
	if got := out[0].Fold.Usage; !reflect.DeepEqual(got, want) {
		t.Fatalf("fold usage %+v,\nwant %+v", got, want)
	}
	plain := FoldWork([]proto.Item{mk(1, proto.KindThinking, ""), mk(2, proto.KindToolUse, "a"), mk(3, proto.KindToolResult, "a"), mk(4, proto.KindThinking, "")})
	if len(plain) != 1 || plain[0].Fold == nil || plain[0].Fold.Usage != nil {
		t.Fatalf("a fold without usage: %+v", plain)
	}
	if b, _ := json.Marshal(plain[0].Fold); bytes.Contains(b, []byte(`"usage"`)) {
		t.Fatalf("an empty usage went on the wire: %s", b)
	}
}

// ReplyUsage reads a line as the node reads a subagent's transcript: sidechain lines like any other, Claude Code's own
// error lines and zeros not at all.
func TestReplyUsage(t *testing.T) {
	for _, c := range []struct {
		line string
		id   string
		ok   bool
	}{
		{claudeReplyLine("a1", "msg_1", "claude-opus-5-5", textBlock, `{"input_tokens":2,"output_tokens":9,"speed":"fast"}`, `,"isSidechain":true`), "msg_1", true},
		{claudeReplyLine("a2", "msg_2", "<synthetic>", textBlock, `{"input_tokens":2,"output_tokens":9}`), "", false},
		{claudeReplyLine("a3", "msg_3", "claude-opus-5-5", textBlock, `{"input_tokens":2,"output_tokens":9}`, `,"isApiErrorMessage":true`), "", false},
		{claudeReplyLine("a4", "msg_4", "claude-opus-5-5", textBlock, `{"input_tokens":0,"output_tokens":0}`), "", false},
		{claudeReplyLine("a5", "msg_5", "claude-opus-5-5", textBlock, ""), "", false},
		{claudeToolResultLine("r1", "toolu_1"), "", false},
	} {
		id, u, ok := ReplyUsage([]byte(c.line))
		if ok != c.ok || ok && (id != c.id || u.Input != 2 || u.Output != 9 || !u.Fast || u.Model != "claude-opus-5-5") {
			t.Errorf("%s: %q %+v %v", c.line, id, u, ok)
		}
	}
}
