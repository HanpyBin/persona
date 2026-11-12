# Knowledge refresh recovery screenshots (#5048)

These four raw captures document the Web Knowledge detail refresh failure and its existing Retry action for [issue #5048](https://github.com/nwverse/persona/issues/5048).

Capture source: `51dfcddf8ee93eeff90d8e102e32f74aff6dece6`. Data comes exclusively from the existing synthetic mock hub; no live account or source is shown. All four images were directly inspected. Standard is the default app text size; Large is the maximum app option (14/13), not an OS/browser maximum. The current contribution preserves the captured project-detail/error presentation; later source reconciliation is identified in its PR.

| Theme | Text size | Raw capture |
| --- | --- | --- |
| Light | Standard | [light-standard.png](light-standard.png) |
| Light | Large | [light-large.png](light-large.png) |
| Dark | Standard | [dark-standard.png](dark-standard.png) |
| Dark | Large | [dark-large.png](dark-large.png) |

The save succeeded, while its following detail read returned a coded 503. The page retains its last good detail and now displays the error and Retry. A separately observed Retry sent one GET, no PUT, then displayed the saved tags and cleared the error.

This branch contains review evidence only and is not intended to merge into main. The code contribution remains on `fix/5048-knowledge-refresh-error`. Links from the PR use this evidence commit as an immutable permalink.
