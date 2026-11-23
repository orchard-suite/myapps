# myapps

A self-hosted My Apps portal, like Microsoft My Apps or Okta's dashboard:
the front door to every application an organization offers its people,
one tile per application a person may open, each showing its live status.
The Landscape shows every application the organization offers, like the
CNCF landscape. The platform team also gets analytics: how the
applications and services cover whatever classifications the organization
measures itself by.

It works for any organization. Everything lives in an
[Orrery](https://github.com/orchard-suite/orrery) catalog: admins keep the list
of applications there through My Apps, paired with what the sources found
(the enterprise apps people sign in to through Entra ID, the catalog's
workloads, the open-source services the organization runs, and the
outside products it uses). My Apps has no database.

## Applications, not services

An **application** is a defined deployment people in the organization use:
installed on their machine (Outlook), cloud-hosted on someone else's
platform (Jira), or self-hosted on ours (Argo CD, a portal built here). It
is not a service. Most services are jobs, APIs, backends, or products sold
to customers, and never appear in My Apps. An application links to the
services it runs: the ones the organization built it from, and through
them their repositories, or the open-source project it is. A SaaS
application links to the outside API it is. It lives in Orrery as
`it_application`, which My Apps offers and admins keep.

| Field or relation | Meaning |
| --- | --- |
| title, icon, `description`, `url` | the name, logo, line, and where people open it (an installed application: where to get it) |
| `hosting` | `self_hosted`, `saas`, or `installed` |
| `self_built` | true when the organization built it itself, wherever it runs; the Landscape draws it as Built here |
| `lifecycle`, `tier` | as elsewhere in the catalog; `planned` draws dimmed and dashed |
| `listed` | `shown`, `no_login`, or `hidden`. Shown is a tile and a Landscape card. `no_login` (Hidden from login) is a Landscape card with no tile, for an application people do not open from the launcher, such as My Apps itself. Hidden stays in the catalog with neither |
| `audience` | front-door roles that may open it |
| `people` | with no Entra app, the emails of people who may open it, for people Entra does not hold too |
| `access`, `access_role`, `access_groups` | how a person gets it: `open` (anyone; its Entra apps' assignments do not limit it, for an application that keeps its own accounts), `direct` (assigned to its first Entra app, in that role), `group` (added to one of those groups), or `package`. With no Entra app, `access_groups` are any Entra groups whose direct members may open it |
| `assign_teams` | the teams that may grant it; mark it only an admin changes in Orrery |
| `sign_in` | the Entra apps people sign in through, test and production alike; anyone any of them admits may open it |
| `services`, `workload`, `api` | the services it runs (built here, or the open-source project), the workload it runs as, the outside API it is |
| `team`, `business_capabilities` | who owns it; where everyone finds it |
| one relation per lens | its place in that classification, such as `pillars` to `plat_pillar` |

## What it shows

- `/` a launcher. A greeting, then one field: type and the best matches
  among your apps and actions rank under it; Enter opens the first. Under
  it the **dock**: your favorites (star any app), then the ones you used
  recently (forget one with its cross), and on a first visit your critical
  and important ones. Then every app you may
  open, one row each (logo, name, one line of what it is), grouped by
  **business capability** (Finance, Shipping, Collaboration: what part of
  the organization it serves); a group of one or two apps shares a row
  with its neighbours. A row shows a status pill only when it is not
  green. Hide an app you never use from its menu: it leaves Home, the
  dock, and the minimap, and waits under **Hidden apps** at the bottom
  (or the "hidden" link by the minimap) until you restore it. Search
  still finds it. An app opens in a new tab, so Home stays open as the
  place everything starts; **Open apps in a new tab** in the account menu
  turns that off. Favorites, hidden apps, recently used, and that choice
  are kept in the browser's local storage only.
- Every page has a **palette**: ⌘K (Ctrl K), or `/` when not typing,
  finds any app you may open, any action, and any page.
- `/landscape` every shown application as one wall, for everyone. See
  **The landscape**. `/suite` moved here.
- `/analytics` for the platform team: one lens at a time over the
  applications and the services. See **Analytics**. `/coverage` moved here.
- `/admin` for admins: the applications and what the sources suggest, the
  capabilities, and the lenses. See **The admin pages**.
- `/requests` for everyone, when a request desk is set: report a problem
  with an app or ask for a change, and see what became of everything you
  sent. See **Requests**.
- `/v1/apps` the tiles a person may open, as JSON.
- `/v1/landscape?lens=<blueprint>` one lens as JSON: `{lens, categories,
  cards, edges, sources, sourcesKnown, catalog}`. A card that is an
  application names it in `app`.
- `/icons/{name}` a logo, read from Orrery (see **Logos**).
- `/apple-touch-icon.png`, `/icon-192.png`, `/icon-512.png`, and
  `/manifest.webmanifest`: what a phone or a desktop reads to add the
  portal as an app with its own icon. They answer without a sign-in, like
  `/assets/`. The icon's source is `internal/server/assets/app-icon.svg`;
  render each PNG from it with `rsvg-convert -w N -h N`.
- `/healthz` and `POST /api/telemetry` on a second port, open to anyone.

Below the tiles, **Things you can do** lists the self-service actions the
signed-in person may run in Orrery, asked of Orrery as that person
(`GET /v1/actions?for=<email>`), each a link to the action's form there.
My Apps never runs an action: Orrery does, with its own approvals and its
own record of who did what. `myapps.actions.read` counts the reads by
outcome.

Reads are cached for `CATALOG_TTL` (30 seconds). When Orrery cannot be
reached, the page says "Catalog unreachable" and keeps showing the last
tiles it read. It reads `it_application`, `business_capability`,
`myapps_mapping`, `myapps_ignored`, the pending `it_approval` records, the
hub's blueprints, and, through the mappings, the people, groups, Entra
apps, assignments, workloads, services, APIs, and teams for the tiles and
the admin pages; and the lenses, each lens's categories,
`myapps_assessment`, the mapped services and APIs, and the sources for the
Landscape and analytics.

### Where it reads each kind of thing

Every estate models people, groups, and the rest in its own way. A
mapping (`myapps_mapping`, one per kind, kept on **Admin, Sources**) says
which blueprint holds each kind and which of its fields and relations
carry each part My Apps reads, and how the hub words a value My Apps acts
on (such as `departed` for someone who has left). `catalog.Mappings`
reads every record through it into My Apps' own names, so nothing past
it knows the estate. A kind never saved reads from the names Orrery's
own pushers use: `entra_user` (`email`, `enabled`), `entra_group`
(`members`, `dynamic`), `entra_app`, `entra_app_role_assignment` (`app`,
`user`, `group`), `sw_workload` (`url`), `sw_service` (`origin`: `built`
or `adopted`), `sw_api` (`external`), and `team`. Assignments may instead
be kept on the Entra apps, as relations to the people and the groups. A
workload's address may be a list of host names, such as an ingress's
`hosts`: the first is read, as https. A
kind whose blueprint the hub lacks is not read, and an application is
offered a relation only to a kind the hub holds, at its mapped blueprint;
once that relation exists, Orrery keeps its target, so the Sources page
refuses to map the kind elsewhere.

## The landscape

`/landscape` is every shown application as one wall, like the CNCF
landscape, for every signed-in person: where people shop for applications.
It names applications, never who holds them, and never a service.

- **The wall**: each top business capability is a bordered box in order;
  inside it, a box per capability under it; an application on no
  capability is under **More apps**, last. A card layers what an
  application is, by shape in its corners. Where it runs (self-hosted, SaaS,
  or installed) is a faint server, cloud, or laptop top left. **Built
  here**, when it is self built or a service it runs was built here, is a
  blue seal with a check top right, and the card glows. **Open source**,
  when it runs an adopted one, is a faint fork bottom right. An
  application may have both, on any hosting. A logo-only card leaves the
  hosting to its hover, and a status that is not healthy takes that
  corner. Its tier sets its size: a
  critical one is large, with its name and every layer in words; an
  important one is medium, with its name; a standard one, or one with no
  tier, is its logo alone. Hover or focus any card for its name and
  layers. A box grows to fill its row, and its cards flow to fill it.
  Planned cards are dashed; one that is not yours is grey with a lock,
  and a large one says how to get it. **-** and **+** step the size,
  kept in the browser. A card opens a drawer with its details.
- **The list**: the same applications as a table: capability, every
  layer, owner, and whether it is yours.
- **Filters**: a search box, kind, in use or planned, and **Only mine**.
  A kind matches every card that has it as any layer, so **Built here**
  finds a self-hosted one and a SaaS one alike.
- **Yours or not**: an application the person may open has **Open** in its
  panel. One they may not is dimmed; its panel carries **Request access**
  when Orrery lets the person run the request action for the way it grants
  access (`request_app_access` for `direct`, `request_group_access` for
  `group`), a link to that action's form in Orrery with the application
  filled in (`?entity=it_application:<key>`); otherwise it says which team
  owns it.
  An application with no tile for anyone, hidden from login or with no
  address, is never dimmed: its panel has no **Open**, and the list says
  **No tile**.
- **A card's panel**: what it is for, its capabilities, the services it is
  built from with their repositories, and its page in Orrery.

## Analytics

`/analytics?lens=<blueprint>`, for the front door's `Platform` and `Admin`
roles, is one lens over the suite: the applications and, here only, the
services built or adopted and the outside APIs, placed on its categories.
A lens is any classification in the hub: business capabilities always,
then each one the platform team adds on the Lenses page, such as
`plat_pillar` or a department's own pillars. Nothing hard-codes a scheme:
a lens's categories are its blueprint's entities, top level only, and a
card is placed by every relation whose target is the lens's blueprint.

- **Strength of each category**: the newest `myapps_assessment` for it, a
  rating from 1 to 5 with what covers it and what is missing, who assessed
  it and when, and how many before. Assess saves a new one; the old ones
  are its history, kept two years.
- **Overlap**: how many cards sit in more than one category, and the pairs
  that share the most.
- **The gaps** and the counts by owning team and by who may open them.
- **Wall, matrix, and analysis**: the cards by category; the cards against
  the categories, with the ones fewer than two hold marked; and a radar of
  how many are placed on each category, in one neutral colour, with a
  table of how each card works with Orrery natively.

A card's integration is the union over every Orrery source whose home is
the card. A source's home is the card its `actsFor` names
(`<blueprint>:<key>`), else the nearest card of the same blueprint whose
key its key sits under.

| Mark | Set when a source whose home is the card |
| --- | --- |
| Data | wrote entities of any blueprint |
| Blueprints | offered any blueprint |
| Actions | put at least one action |

A failed read of the sources does not fail analytics: every integration
is unknown, the view says so, and the categories stay current.

## Requests

With a [Graft](https://github.com/orchard-suite/graft) request desk set
(`GRAFT_URL`, `GRAFT_TOKEN_FILE`, and `PUBLIC_URL`), the account menu
carries **Report a problem** and **My requests**, both at `/requests`.
Without all three the feature is off: no menu entry, no route, and one log
line at start that names what is not set.

- **Report a problem or ask for a change.** Pick one of your apps, write
  what happened or what you want (10 to 16,000 characters), and send. My
  Apps' server hands it to Graft's token port (`POST /v1/requests`) with
  its own intake token, read from `GRAFT_TOKEN_FILE` on every call, so a
  rotated token works without a restart.
- **My requests.** What you sent through My Apps, newest first, as Graft
  answers it (`GET /v1/requests?person=`): each with its title, where it
  stands in plain words, Graft's answer, and a link to its ticket when it
  has one. Nothing is kept in My Apps.

The browser never says who the person is. The person is the one the front
door signed in, from the identity header. The form carries two things: the
app picked and the words. My Apps sends Graft the person's sign-in
address, their words, the app's repository, the app's address, and the
browser's name (its user agent). Nothing else in the form is read.

**Which repository a request goes to.** An application names no repository
of its own. Its `services` relation names the services it runs; the first
of them that the organization built (`origin` of `built`) and that has a
`repository_url` decides, read through the services mapping. The path of
that address, as the forge names it (`acme/portal` from
`https://git.example.com/acme/portal`), is sent as `repository`. An
application with no such service is not offered in the form. A service
someone else built never decides: its repository is theirs.

A send is a `POST` taken only from a page whose origin is `PUBLIC_URL`.
When Graft refuses a request in words meant for the person (the text is
too short or too long, they hold no lane on that repository, they sent too
many this hour), the form shows those words and keeps what they wrote. Any
other failure says only that the desk could not be reached. While an admin
views the portal as someone else, the page asks Graft nothing and shows
no requests.

No log line and no trace span carries a person's address or the token.
`myapps.graft.call` counts the calls by operation (`send`, `list`) and
outcome (`ok`, `refused`, `failed`).

## Logos

An entity's `icon` names an icon in Orrery: a custom icon or one of its
built-in brands. Every time the tiles or the landscape are read, My Apps
reads Orrery's icon index once: the hub's release (`GET /v1/whoami`),
which fixes every built-in brand, and each custom icon's digest (`GET
/v1/icons`). It then reads into memory (`GET /v1/icons/{name}`, as
`ORRERY_TOKEN`, eight at a time, with the ETag it holds) only the icons
that are new, whose digest changed, or, after the hub's release changes,
the built-in ones. An unchanged catalog costs two calls a read, however
many icons it names. A failed read keeps the last good copy and is asked
again on the next read; a failed index asks for nothing; an icon Orrery
no longer has is dropped. Only names that
match `^[a-z0-9][a-z0-9_]{0,62}$` are asked for, and only SVG, PNG, or
WebP up to 256 KiB are kept. Each icon is held in two copies: one for a
dark background, and one for a light background (`?theme=light`). They
differ only for a brand mark drawn in black, which Orrery inks light by
default.

The page draws a logo, as an `<img>` from `/icons/{name}`, only for an
icon it holds, and a monogram otherwise. The landscape always draws on a
light ground, so it loads `/icons/{name}?theme=light`; a tile on the home
page follows the system theme. `/icons/{name}` answers from
memory, never from Orrery, with the stored type, `Cache-Control: private,
max-age=300`, `X-Content-Type-Options: nosniff`, the ETag, and for SVG
`Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'`.
`myapps.icon.read` counts the reads by outcome: for an icon `ok`,
`not_modified`, `not_found`, or `failed`; for the index `index` or
`index_failed`.

## Who sees what

myapps sits behind an identity-aware proxy (the front door) that sets `X-Forwarded-Email` and `X-Forwarded-Groups` (comma
separated) on every request. The session port trusts those headers and
nothing else, so only the proxy may reach it; in Kubernetes a NetworkPolicy
does that.

- An application with Entra apps, unless its `access` is `open`, opens
  only for someone one of them lets sign in: anyone in the tenant when it needs no assignment, else someone
  assigned to it directly or through a group they are directly in. Entra
  does not pass app access through nested groups, so neither does My Apps.
  The person is the one the people mapping holds whose `email`, or one of
  whose `aliases` (such as mail aliases), is the front door's; an alias
  never takes another person's email. Someone Orrery does not hold sees only the ones open to everyone. Who is
  assigned never leaves the server.
- An application with no Entra app, or one whose `access` is `open`, is
  for everyone signed in until it names someone. Once it names a front-door role (`audience`), an Entra
  group (`access_groups`), or a person (`people`), it opens for anyone who
  holds one of those roles, is directly in one of those groups, or whose
  front-door email is one of those people; roles and emails are matched
  without regard to case. A group decides through Orrery's Entra data, so
  its list is kept in Entra; a person is named in My Apps, and need not be
  in Entra at all. The people named never leave the server.

Every decision is logged, both ways. An application the person may not
open does not exist for them on Home or at `/open/{key}` (404); the
Landscape draws it, dimmed.

## Viewing as someone else

An admin checks the access rules by seeing the portal as another person.
The account menu's **View as someone** takes the person's email (Entra's
people are offered) and, optionally, the front-door roles to give them,
since My Apps learns a person's roles only from their own requests. Until
**Exit**, or eight hours, every page, `/open/{key}`, and the APIs answer as
they would for that person: their tiles, their locks on the Landscape, and
only the pages their roles open. A strip under the bar says whose view it
is. Nothing changes while viewing: every change is refused, opens are not
counted, and the browser keeps the admin's own favorites and recents. The
view lives in an HttpOnly cookie that counts only while the front door
says the caller is an admin, and the logs name the admin
(`view as started`, `viewing as`, `view as ended`).

## The admin pages

- **Applications** (`/admin`, `Admin` only): two sheets, and the page
  fills the window: only the grid or the list scrolls, never the page.
  - **Added**: every application in one **grid**: a row each, a column
    for every field and every lens. Click a cell to change it where it
    is: a dropdown for one choice, a list of toggles for several, a field
    for words, and three buttons for In My Apps. A change saves to Orrery
    as soon as it is picked or Enter is pressed, one field at a time
    (`POST /admin/applications/{key}` with only that field, answered with
    the saved row as JSON); Esc puts it back, and a refused change says
    why under the grid. Each column's header sorts it and lists every
    value it holds with how many applications hold it, to filter by.
    Which columns show is kept in the browser. Type a name beside the
    search to **add an application**, and fill in its cells. A new
    application's key is a slug of its name and never changes. A row's
    menu opens its address or its Orrery page, or removes it.
  - **Waiting** (`/admin?sheet=waiting`): what the sources found that no
    application is paired with yet: Entra apps, workloads with an
    address, open-source services the organization runs (`sw_service` of
    origin `adopted`), and outside products it uses (external `sw_api`).
    Services built here are never suggested. Each has **Make it a new
    application** (filled in from it), **Attach** (an Entra app becomes
    one it signs in through, a workload the one it runs as, a service one
    it runs, an API the one it is), or **Dismiss** (kept as a
    `myapps_ignored` record, so it stops coming back). Pick several to
    **Make applications** at once, on a business capability if one is
    picked: each keeps where its source is placed on every lens, such as
    its pillars.
- **Capabilities** (`Admin` only): the tree of business capabilities, to
  add, rename, nest, reorder, and remove. A rename keeps the key.
- **Sources** (`Admin` only): where My Apps reads each kind of thing (see
  above). Pick a blueprint and save; then pick which of its fields and
  relations carry each part. Saving offers applications their relation
  to it again.
- **Lenses** (`Platform` and `Admin`): the classifications analytics offers
  beyond business capabilities. Any blueprint whose entities carry an
  `order` can be one; adding one also gives applications a relation to it
  (named after its thing, plural, such as `pillars`), so the application
  form can place them on it.

Everything My Apps writes is its own: `it_application`,
`business_capability`, `myapps_lens`, `myapps_assessment`,
`myapps_mapping`, and `myapps_ignored`, with the push token. It never writes a pusher's entity.
It offers those blueprints, and an `application` relation on
`it_approval`, when it starts (`catalog.CurationBlueprints`, linked as the
mappings read with `ORRERY_TOKEN` say), and offers a refused one again
every ten minutes until the hub takes it. A change is
taken only from a page whose origin is `PUBLIC_URL`.

## Running it

One binary, or the image `ghcr.io/orchard-suite/myapps:<version>` (linux/amd64),
signed with cosign by `^https://github.com/orchard-suite/myapps/`.

```bash
ORRERY_URL=https://orrery-ingest.example.com ORRERY_TOKEN=... ORG_NAME="Example" ./myapps
curl -H 'X-Forwarded-Email: user@example.com' -H 'X-Forwarded-Groups: Staff' localhost:8080/v1/apps
```

| Variable | Meaning | Default |
| --- | --- | --- |
| `ORRERY_URL` | the Orrery ingest service's base, such as `https://orrery-ingest.example.com`; the reads and the writes add `/v1` themselves | required |
| `ORRERY_TOKEN` | a bearer token that may read entities and `GET /v1/sources`; its role also holds `actions.list` to show each person their actions | required |
| `ORRERY_PUSH_TOKEN` | a source token whose source is registered for `it_application`, `business_capability`, `myapps_lens`, `myapps_assessment`, `myapps_mapping`, `myapps_ignored`, and `it_approval` (and may delete `myapps_lens`, `myapps_assessment`, `myapps_ignored`, `business_capability`, and `it_application`), with no cadence: it writes only at start and when an admin decides. Set with `ORRERY_URL`, My Apps offers `catalog.CurationBlueprints` at start and saves what admins decide. `ORRERY_TOKEN` still does every read | unset: the admin pages only read |
| `PUBLIC_URL` | the portal's own origin, such as `https://myapps.example.com`. A change on the admin pages, and a request sent to the desk, is taken only from a page with this origin | unset: the admin pages only read, and no requests |
| `ORRERY_PUBLIC_URL` | where people open Orrery, such as `https://orrery.example.com`. Set, the page lists the self-service actions the person may run, each a link into Orrery's form for it; Request access links to Orrery's form; the approvals count links to Orrery's list; and each card links to its page there | unset: no actions, requests, approvals, or Orrery links |
| `GRAFT_URL` | the Graft request desk's token port, such as `http://graft.example.com:8081`. Set with `GRAFT_TOKEN_FILE` and `PUBLIC_URL`, people report problems and see their requests at `/requests` | unset: no requests |
| `GRAFT_TOKEN_FILE` | the path of a file that holds My Apps' intake token for Graft, and nothing else. It is read on every call. The token itself is never an environment variable | unset: no requests |
| `SIGNOUT_URL` | where the front door signs a person out of this app, such as `/oauth2/sign_out`; set, the header carries Sign out | unset: no sign-out link |
| `ORG_NAME` | the name over the tiles | `My Apps` |
| `ORG_ICON` | the organization's logo, an icon name in Orrery, drawn in place of My Apps' mark once read | unset: My Apps' mark |
| `CATALOG_TTL` | how long the tiles and the analytics are cached | `30s` |
| `SESSION_ADDR` | the pages, behind the proxy | `:8080` |
| `OPEN_ADDR` | health and the telemetry relay | `:8081` |
| `LOG_LEVEL` | `debug`, `info`, `warn`, `error` | `info` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP traces and metrics go here; unset, nowhere | off |
| `OTEL_SERVICE_NAME` | the service name on the OTLP resource | `myapps` |
| `FARO_URL` | the Grafana Faro collector the browser's telemetry is relayed to; set, the page loads the Faro SDK | off |
| `FARO_API_KEY` | the collector's key; without it batches are accepted and dropped | off |

## Telemetry

JSON logs on stdout with `trace_id`. OTLP spans for every request and
every Orrery call. Metrics: `http.server.request.duration`,
`myapps.build_info`, `myapps.catalog.read` by outcome,
`myapps.landscape.read` by outcome, `myapps.icon.read` by outcome,
`myapps.catalog.age`, `myapps.tile.open` by top business capability,
`myapps.admin.change` by kind and outcome, `myapps.graft.call` by
operation and outcome, a client span for every call to Graft named for
its operation, never its address,
`myapps.browser.batches` by outcome, `process.cpu.time`, and
`process.memory.usage`. The page runs the Grafana Faro SDK as `myapps-web`,
posting to `/api/telemetry`. Its events are `tile.open` by top business
capability, `landscape.open` by the lens's category (and origin: the
one kind the card is counted under: built here, else open source, else
its hosting), `landscape.filter` by filter and
value (the zoom is filter `zoom`, value `in` or `out`; the view is filter
`view`, value `grid`, `list`, `matrix`, or `analysis`), `apps.search`, `action.open`, and
`palette.open`, every value from a fixed list; the server caps a batch at 256 KB, names the
app itself, strips any user, and relays with the key. Everything is off
without its variable. The gate, the relay, the Orrery client, and the
telemetry setup come from `github.com/orchard-suite/platformkit`.

## Deploying it

An estate deploys the image with its own manifests, alerts, and
dashboards. The session port must be reachable only through the proxy.

## Developing

```bash
make check   # gofmt, go vet, go test
make build   # dist/myapps
make docker  # the linux/amd64 image
```

`AGENTS.md` has the shape and the rules.
