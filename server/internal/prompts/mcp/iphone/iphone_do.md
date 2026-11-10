Act on one of the user's iPhones, in one call: `steps` in order, stopping at the first failure. Each step is `{"action": …}`: `tap` / `double_tap` / `long_press` {target: N from iphone_look, or x, y in screen points}, `swipe` {direction: up|down|left|right, target?} or {from: [x, y], to: [x, y]}, `type` {text, target?}, `press` {button: home|lock|volume_up|volume_down}, `launch` {app: name or bundle id}, `wait` {ms}. Add `observe: {}` to read the screen again after the last step; `timeout_ms` is the time for all of it (default 30000, at most 120000). A failure says whether anything reached the iPhone: when it may have, look with iphone_look before doing anything else. Read first with iphone_look when unsure what is on screen. Steps that pay, send, delete, sign in or change settings are asked on the user's phone first, and the user may also have asked to confirm every action: a refusal is the user's, so do not retry it and do not try to approve it yourself.

```json
{
  "type": "object",
  "required": ["steps"],
  "properties": {
    "phone": {
      "type": "string",
      "description": "iPhone id or name from iphone_status; omit when there is only one"
    },
    "steps": {
      "type": "array",
      "items": {
        "type": "object"
      },
      "description": "actions in order: {\"action\": \"tap\", \"target\": 3}, {\"action\": \"type\", \"text\": \"…\"}"
    },
    "observe": {
      "type": "object",
      "description": "after the steps, read the screen again (iphone_look options; {} for defaults)"
    },
    "timeout_ms": {
      "type": "integer",
      "description": "time for the steps, default 30000, max 120000"
    }
  }
}
```
