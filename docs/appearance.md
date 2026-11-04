# Panel appearance

**Default** is the only theme. It uses the former shadcn/ui preset: neutral
zinc surfaces, white primary buttons, crisp borders and six-pixel corners.
All previous saved theme preferences automatically resolve to Default,
without deleting browser storage. The file editor remains dark.

Open **Settings > Appearance** to see the Default preview. No external
component libraries, fonts or theme assets are downloaded.

The existing Makefile remains the build entry point. Run theme checks with:

```text
node --test scripts/appearance.test.cjs scripts/theme-toggle.test.cjs
```
