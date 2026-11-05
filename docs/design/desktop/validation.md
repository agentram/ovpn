# Design validation record

This record covers the design prototype, not native desktop support or production operations. All displayed identities, nodes and measurements are synthetic.

Checked in the browser:

- English default after reload; Russian selected in Settings; switching back restores English. Both themes render.
- Overview separates four running services from three successful traffic checks.
- Incident → evidence → reviewed plan → simulated operation → verified outcome.
- Server wizard includes host fingerprint acknowledgement, profile, inherited user count and impact before simulated deployment.
- User creation shows all enabled-server scope and preserves the selected quota/expiry in the resulting row.
- Unpublished subscription state is distinguished from available delivery; revocation leaves VPN access active; a revoked subscription offers a new URL, not recovery of the old token.
- `Ctrl+K` search and Enter navigate to a selected action/screen.
- Local and remote checker fixtures can report different results; UDP/DNS remain unverified.
- First-run workspace exposes the setup journey without invented healthy measurements.
- 1024×720 layout has no horizontal page overflow; tables own their overflow. The temporary browser viewport override was reset.
- Browser console reported no errors during the final review. Final preview remains in English/dark mode.

Static checks: JavaScript syntax, JSON example parsing, local document links and default-English source checks passed. Nine representative text/surface contrast pairs met 4.5:1; the lowest tested ratio was 5.17:1. This is not a complete accessibility certification.

The existing CLI cross-built for Windows amd64 with CGO disabled. No Wails native application, installer, OS credential integration, real checker adapter or machine API was built by this proposal. Native three-platform, screen-reader, scaling, real SSH and client-app tests remain roadmap gates.
