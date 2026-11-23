# myapps

The My Apps portal: the front door to every application an organization
offers its people. One page of tiles, one per application a person may
open, grouped by business capability; a CNCF-style Landscape of every
application; and analytics for the platform team through any
classification. An application is a deployment people use, never a
service. Everything lives in Orrery; admins keep the applications there
through My Apps. Read `README.md` first.

This repository is public and estate-neutral. Every name in a test, an
example, or a doc comes from the fixture world:
https://github.com/orchard-suite/hedge/blob/main/docs/fixtures.md.

## Shape

- `cmd/myapps` is the server. One Go module, one static binary, templates
  and assets embedded.
- `internal/server` answers on two ports, one per kind of caller:
  - `Session` (8080): the pages (`/`, `/landscape`, `/analytics`,
    `/admin`, `/admin/capabilities`, `/admin/lenses`), `/v1/apps`,
    `/v1/landscape`, `/open/{key}`, `/icons/{name}`, `/assets/`, the install files
    (`/apple-touch-icon.png`, `/icon-192.png`, `/icon-512.png`,
    `/manifest.webmanifest`, which answer without the gate), and the
    changes (`POST` under `/admin/` and `/analytics/assess`), and
    `POST /view-as` and `/view-as/exit`. With a request desk set, also
    `GET /requests` and the send, `POST /requests`. `/suite` moves
    to `/landscape`, `/coverage` to `/analytics`. Trusts the front door's
    `X-Forwarded-Email` and `X-Forwarded-Groups` headers, so only the
    front door may reach it. Roles: everyone signed in, `Platform`
    (analytics, lenses), `Admin` (everything).
  - `Open` (8081): `/healthz` and `POST /api/telemetry`. Trusts nothing and
    identifies no one.
- `internal/catalog` is the model and every rule over it, in pure
  functions with tests:
  - `application.go`: an application (`it_application`), its tiles
    (`Tiles`), and an application as My Apps writes it (`AppDetails`).
  - `suggestion.go`: what the sources found that no application is paired
    with: Entra apps, workloads, adopted services, and external APIs
    (`EntraApp`, `Suggestion`, `Found`, `SuggestionsOf`,
    `Suggestion.Details`, `myapps_ignored`).
  - `access.go`: who holds what in Entra, from Orrery's `entra_user`,
    `entra_group`, and `entra_app_role_assignment` (`Access`, `SignIn`).
  - `catalog.go`: a category (any classification's entity, with `parent`
    for a tree), a tile (`App`), who may see it (`Viewer`, `VisibleTo`,
    `Filter`), and the tiles grouped by top capability (`GroupBy`).
  - `lens.go`: lenses, the hub's blueprints and the relations that point
    at a lens (`RelationsTo`), business capabilities as My Apps writes
    them, the coverage read (`Coverage`), and `BreakdownOf`.
  - `curation.go`: assessments (`myapps_assessment`, `RatingsOf`),
    `OverlapOf`, the relation a lens needs on applications
    (`LensRelation`), and everything My Apps offers (`CurationBlueprints`).
  - `landscape.go`: one lens shaped into categories, cards, and edges
    (`ShapeLandscape`: applications only for the Landscape, with services
    and APIs for analytics), and the layouts (`Grid`, `MatrixOf`,
    `Details`). `integration.go` and `analysis.go` are the integration
    marks and the radar; `reach.go` what one person may do with each
    application (`ReachOf`).
  - `mapping.go`: where the hub holds each kind of thing My Apps reads
    (`MappingSpecs`, `Mappings`, `myapps_mapping`), reading a record into
    My Apps' own names (`Normalize`, `AssignmentsOnApps`), and checking a
    mapping against the hub (`Check`, `CheckOnApps`).
  - `repository.go`: where an application's code lives (`RepositoryPath`,
    `WithRepositories`): the repository of the first service it runs that
    was built here, as the forge names it.
  - `orrery.go`: the reads (`Directory`, `Coverage`, `Mappings`) and the
    writes (`Curator`, `OrreryCurator`).
  - Tests read `internal/catalog/testdata/products.json`,
    `landscape.json`, and `sources.json`.
- `internal/catalog/icons.go` is the icon cache (`Icons`): every icon the
  tiles and cards name, read when they are read (`WithIcons`), revalidated
  by ETag after the TTL, kept through a failure. `IconSource` is what it
  reads from. `internal/catalog/orrery_icons.go` is the one place that
  reads an icon from Orrery, through the kit's client.
- `internal/server/mappings.go` is the Sources tab: each kind laid out
  against the hub's blueprints (`mappingViews`) and an admin's pick saved
  (`saveMapping`).
- `internal/graft` is the one place that calls the Graft request desk
  (`Client.Send`, `Client.List`), with My Apps' intake token read from a
  file on every call, and a request as a person reads it (`Request.Status`,
  `Request.Heading`). `internal/server/requests.go` is the page: the form,
  the person's requests, and the send.
- `internal/server/grid.go` is the admin grid's model: its columns and
  their choices (`gridColumns`) and one application's row (`rowOf`).
  `assets/grid.js` only draws it, and sends a changed cell back to
  `saveApplication`, which applies only the fields a request names
  (`applyForm`).
- `internal/server` also holds the portal's own metrics
  (`myapps.catalog.read`, `myapps.landscape.read`, `myapps.actions.read`,
  `myapps.icon.read`, `myapps.catalog.age`, `myapps.tile.open`,
  `myapps.admin.change`, `myapps.graft.call`). Their
  names are a contract with the deployment; a rename is a breaking change
  to the dashboards and alerts an estate builds on them.
- The front-door gate, the Faro relay, the Orrery client, and the logs,
  OTLP setup, `myapps.build_info`, `myapps.browser.batches`, process
  metrics, and instrumented HTTP come from `github.com/orchard-suite/platformkit`
  (`door`, `faro`, `orrery`, `telemetry`). A person's groups are
  `door.From(ctx).Roles`.
- `orrery.yaml` at the root is what this repository builds. The app never
  reads or writes it: redgreen reads it from the repository and writes the
  service for the estate that reads this repository.
- `ORRERY_PUSH_TOKEN`, with `ORRERY_URL`, offers `CurationBlueprints` at
  start and writes what admins decide, all of it My Apps' own entities:
  applications, capabilities, lenses, assessments, mappings, approvals,
  and dismissed suggestions.
  `ORRERY_TOKEN` only reads.
- `ORRERY_URL` is the hub's base; the kit's client adds `/v1`.
- A change to a port, an env var, a metric, or a route is a change to
  every estate's deployment: say it in the commit.

## Rules

- Never let a header set identity on the open port, and never add a route
  to the session port that skips the gate, past `/assets/` and the install
  files (`installFiles`), which identify no one.
- Visibility is decided in one place, `catalog.App.VisibleTo`, and every
  decision is logged both ways. An application is a tile only when it is
  `listed: shown`. One with Entra apps opens only for someone one of them
  admits, read from Orrery, unless its access is `open`; a person Orrery does not hold gets only the
  ones open to everyone. Who is assigned never leaves the server. A tile
  the person may not open does not exist for them on Home or at
  `/open/{key}` (404, not 403); the Landscape draws it, dimmed.
  One with no Entra app, or open, is for everyone until it names a front-door role,
  an Entra group (`access_groups`), or a person's email (`people`); then
  only for someone matching one of them. The people named never leave the
  server.
  `listed: no_login` (Hidden from login) is a Landscape card with no
  tile for anyone, never dimmed. `catalog.Listed` decides both.
- An admin may view the portal as another person, read-only, to check the
  access rules. `viewas.go` is the one place it happens: `viewAs` wraps the
  session port and, for a request whose front door says Admin and that
  holds the `myapps_view_as` cookie, puts that person's email and the
  roles the admin gave on the request before any gate reads it. A cookie
  anyone else holds is dropped. Anything but a read is refused while
  viewing, except starting and ending a view; opens are not counted, and
  the page script sends no events and keeps the admin's local storage as
  it was. Every page says whose view it is, with Exit. The logs name the
  admin.
- A request to the desk is sent as the person the front door signed in,
  and the desk is asked only about them. Nothing the browser sends names
  the person, the repository, or the lane: the form's app and words are all
  that is read, and the app must be one of the person's tiles with a
  repository. Only what the desk says for the person (400, 403, 429) is
  shown to them. A view as someone else asks the desk nothing. No log line
  or span carries a person's address or the token: the desk's client names
  its operation, never the address it called.
- An application is not a service. Services never appear on Home or the
  Landscape; they are what an application runs, and analytics counts them.
  The admin queue suggests only the open-source services the organization
  runs and the external APIs, never a service built here.
- My Apps never asks Microsoft Graph and holds no Azure identity: a
  pusher writes who holds what. My Apps writes only its own entities,
  never a pusher's.
- No estate's blueprint or field names are built in past the defaults in
  `catalog.DefaultMappings`. Everything My Apps reads that it does not own
  is read through `catalog.Mappings`, by kind; code past `Normalize` uses
  My Apps' own names only. An application relates to a kind only at its
  mapped blueprint, offered only when the hub holds it.
- No classification is hard-coded. Business capabilities (`business_capability`)
  group what everyone sees; any other classification is a lens the
  platform team adds, read from its own blueprint, placed by the relations
  that point at it. A category's key in URLs, JSON, and events is its
  entity key. An application or card placed on nothing known goes under
  Other ("More apps"), last.
- Orrery is read through a cache only, never per request: one for the
  tiles, one for the coverage, and the icons they name. A failed read
  keeps the last good one and says so on the page. `/icons/{name}`
  answers only from memory.
- Logos are `<img>` only, from `/icons/{name}`, drawn only for an icon
  held; otherwise a monogram. Never inline an icon's markup.
- Recently used, favorites, hidden apps, zoom, and the admin grid's hidden columns live in
  the browser's local storage only.
- Every lens, the reach, and the breakdown are shaped and laid out in
  `internal/catalog`, in pure functions with tests; the template only
  draws. The radar's every point position is laid out there too; the page
  script only picks one as it filters. Browser events carry values only
  from lists the server drew (capability and category keys, kinds,
  adoptions, views: `grid`, `matrix`, `analysis`).
- The analysis shows evidence, never a claim: rows are applications and
  services built here, a dot is a placement in the catalog, a
  mark is what Orrery saw a source acting for the card write. A count is
  not maturity; the radar uses one neutral colour, never red or green. A
  failed sources read leaves the integration unknown, never the
  coverage failed.
- Every tile opens through `/open/{key}`, so the server counts each open
  by top business capability; the browser pushes the same event to Faro.
  Every app link the server draws opens a new tab; `aim` in
  `assets/app.js` takes the target off when the browser's
  `myapps.newtab` is `off`, and gives a link it builds the same target.
- Logs are JSON lines on stdout. Never a token, a secret, or more of a
  person than their email.
- No continuously repainting CSS animations. Light and dark follow the
  system. Phone first.
- No em dashes or en dashes in anything a person reads. Never a real
  person's name or email in fixtures or docs: `user@example.com`.
- `VERSION` is the version of record. Never edit it, the manifest, or
  `CHANGELOG.md` by hand.

## Delivery

Agents push to main.

Commit with conventional commits scoped `myapps` (`feat(myapps): ...`,
`fix(myapps): ...`) and land with `make land` (`make land BRANCH=<branch>`
from a worktree). It runs `make check`, then `release-damnit land`. CI runs
the same check on every push and pull request (`test.yml`); the release
calls it first, then versions every `feat` and `fix`, tags it, and builds
and signs `ghcr.io/orchard-suite/myapps:<version>` for `linux/amd64`.

## Checks

```bash
make check
```

Run it before every commit; `make land` runs it too, and CI runs it on
every push. It needs Go (see `go.mod`). To see it against a
real catalog, `make build` and run `dist/myapps` with `ORRERY_URL` and
`ORRERY_TOKEN` set, then call it with the front door's headers as the
`Makefile` shows.
