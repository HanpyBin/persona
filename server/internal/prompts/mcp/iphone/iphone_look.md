See one of the user's iPhones (one iphone_status lists): what is on its screen as numbered elements `[N]` (role, label, value), plus a screenshot. Act on them in iphone_do with `"target": N`; numbers stay valid in this conversation until the screen is read again. A long list is cut: pass `query` to keep only matching elements. Changes nothing. A locked iPhone shows only its lock screen: ask the user to unlock it.

```json
{
  "type": "object",
  "properties": {
    "phone": {
      "type": "string",
      "description": "iPhone id or name from iphone_status; omit when there is only one"
    },
    "query": {
      "type": "string",
      "description": "keep only elements containing this (case-insensitive)"
    },
    "screenshot": {
      "type": "boolean",
      "description": "default true; false returns the elements only"
    }
  }
}
```
