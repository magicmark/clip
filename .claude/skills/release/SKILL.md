---
name: release
description: Release a new version of clip. Use when the user asks to publish, release, cut a release, bump the version, or ship a new version.
---

## To cut a release

1. Determine the current version from `Makefile` (the `VERSION` variable) 
2. Ask the user what the new version should be, suggesting the next patch/minor/major bump as appropriate.
3. Update the `VERSION` line in `Makefile` to the new version (format: `vX.Y.Z`).
4. Commit the version bump to the current branch.
5. Send a pull request using the /gh skill
6. Ensure the pull request title is succinct and one sentence that describes the most important thing being changed/added

## Important

- Do NOT run `publish.sh` manually or create git tags — CI handles both.
- Do NOT push directly to main. Create a PR if not already on main.
- Don't worry about git tags
