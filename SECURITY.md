# Security Policy

## Scope

MOSAIC's primary attack surface is its Go tooling (`Tools/`) and hook scripts (`Catalog/Hooks/`), which execute code in users' environments. The agent templates themselves (markdown files) carry minimal risk.

## Reporting a Vulnerability

If you discover a security issue, **please do not open a public issue.**

Instead, email **tomasgurtler21@gmail.com** with:

- A description of the vulnerability
- Steps to reproduce it
- The affected component (tool, hook, deployment script, etc.)

I'll acknowledge your report within 7 days and work with you on a fix before any public disclosure.

## Supported Versions

Only the latest release on `main` is actively maintained. Security fixes will not be backported to older versions.
