# @agscale/agscale

NPM wrapper for AGScale CLI.

It downloads the matching native binary from GitHub Releases on first run and executes it.

## Usage

```bash
npx @agscale/agscale version
```

Override release source if needed:

```bash
AGSCALE_REPO=yangpuyu/agscale AGSCALE_RELEASE_TAG=v0.3.1 npx @agscale/agscale version
```
