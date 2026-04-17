# Packaging Templates

This directory contains repo-local publication scaffolding for package managers that still require
submitting manifests to an external registry.

Currently included:

- `winget/` manifest templates
- `scoop/` manifest template
- `chocolatey/` nuspec and install templates

Render release-specific manifests with:

```bash
./scripts/render_release_manifests.sh --version v0.3.0 --windows-amd64-sha256 <sha256>
```

That writes ready-to-review files under `packaging/generated/<version>/`.

What this does not do:

- create or publish a Homebrew tap
- submit to Winget, Scoop, or Chocolatey
- create a tagged GitHub release

Those are still external release steps, but the generated files are intended to remove manual
copy-editing from the publication flow.
