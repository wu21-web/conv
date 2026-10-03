# Security Policy

This markdown file contains security policy for `wu21-web/conv`.

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| Last Release   | :white_check_mark: |
| Preleases   | :x:                |

## Understanding Your Vulnerability Report

As mentioned in the project [README.md](./README.md), **most vulnerabilities of this project is inherited from the backend commands.**

For example, if `ffmpeg` has a vulnerability allowing RCE when converting a video file, `conv` might inherit that vulnerabilty.
However, `conv` maintainers have nothing to do about it, the only workaround is to disable this version of `ffmpeg` and prompt users to upgrade the command to a non-vulnerable / patched version.

Therefore, before reporting a vulnerability, try upgrading all dependencies to the latest release, like `ffmpeg` and `imagemagick` binaries to the latest, patched versions.
If the vulnerability is still effective or the PoC still works, you can file in a private vulnerability report through the _Private Vulnerability Report Feature_ in Github.

> [!NOTE]
> If you belief that this vulnerability inheritance causes severe security issues, please open an issue, including the advisory link, the affected backend, and the version number (SHA / tag).

## Roadmap

Note that we might add a `vulnerablities.json` to disable vulnerable versions of backend binaries. 
A `--[no-]safe` flag might be implemented in a later release to bypass this safety restriction.

## Reporting a Vulnerability

File in a private vulnerability report through the _Private Vulnerability Report Feature_ in Github.
