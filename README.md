# ampworth

The command line for [Ampworth](https://ampworth.com): UK solar PV and heat
pump calculators that follow the published MCS standards and show their
working, and the guides that explain them. It also runs as a local MCP server,
so AI assistants can use the same tools.

```sh
$ ampworth run heat-pump-size --location birmingham --annual-fuel-use 12000 --bedrooms 3 --quoted-heat-pump-kw 12
Estimated peak heat loss: 3.5 kW, so the heat pump has to deliver at least that at -3.2 C outside and 45 C flow.
...
```

It's a thin client for Ampworth's public API. The calculations run on
ampworth.com, so the CLI always gives the same answers as the website, and it
needs an internet connection.

## Install

With Homebrew, on macOS or Linux:

```sh
brew install ampworth/tap/ampworth
```

With Go 1.27 or later:

```sh
go install github.com/ampworth/cli/cmd/ampworth@latest
```

Or download a binary for macOS, Linux or Windows from the
[releases page](https://github.com/ampworth/cli/releases).

## Use

```sh
ampworth list                       # the calculators
ampworth describe solar-pv-output   # the fields a calculator takes
ampworth run solar-pv-output --postcode "BS1 5TR" --capacity-kwp 4.4 --orientation 30 --inclination 35
ampworth search "flow temperature"  # search the guides, articles and questions
ampworth docs --kind guide          # list the documents
ampworth read guides/sizing-a-heat-pump
```

Fields are flags named after the calculator's fields, in kebab case
(`--capacity-kwp`) or as written in the API (`--capacityKwp`). A field that
takes a list, such as a room's elements, takes its JSON array as the value. A
yes-or-no field given on its own, such as `--include-battery`, means yes.

Add `--json` to any command for the API's JSON instead of text. Use
`--api <url>`, or set `AMPWORTH_API`, to point at a different copy of the API.

## Calculators

| Calculator | What it works out | Method |
| --- | --- | --- |
| `solar-pv-system-size` | The solar array a home needs, with panels, battery and savings, from a postcode, roof direction and a year of electricity use | MIS 3002 |
| `heat-pump-size` | A house's heat loss from its fuel bills, and whether a quoted heat pump looks the right size | MIS 3005-D |
| `solar-pv-output` | Annual generation of a known array | MIS 3002 |
| `solar-pv-shading` | Shade factor from the sunpath chart | MGD 005 |
| `solar-pv-self-consumption` | How much generation is used at home, with or without a battery | MGD 003 |
| `room-heat-loss` | Design heat loss for one room | MIS 3005-D |
| `radiator-flow-temperature` | The flow temperature a radiator needs | MGD 007 |
| `hot-water-cylinder` | Hot water demand and cylinder size | MIS 3005-D |
| `heat-pump-noise` | Sound level at a neighbour's window, for permitted development | MCS 020 |
| `heat-pump-running-cost` | Running cost compared with the current fuel | MCS 007 |
| `connection-route` | Whether solar panels and batteries can connect under G98 or need G99 approval | ENA EREC G98 / G99 |

The list comes from the API each time, so a calculator added to Ampworth
appears here without a new release.

## Use it with an AI assistant

`ampworth mcp` runs a local MCP server over standard input and output, with
every calculator as a tool, plus `list_calculators`, `search_documents`,
`get_document` and `list_documents`. Add it to any client that starts local
servers:

```json
{
  "mcpServers": {
    "ampworth": {
      "command": "ampworth",
      "args": ["mcp"]
    }
  }
}
```

**Claude Code:**

```sh
claude mcp add ampworth -- ampworth mcp
```

If your client connects to remote servers, you don't need the CLI: use
`https://ampworth.com/mcp` directly. See
[ampworth/mcp](https://github.com/ampworth/mcp). For guided workflows, such as
checking a heat pump quote, see [ampworth/skills](https://github.com/ampworth/skills).

## Data

The CLI sends only the inputs each request needs, such as a postcode, a roof
direction or a year of energy use, to `https://ampworth.com/api/v1`. It stores
nothing and sends nothing else. The server works the answer out and doesn't
store the request. Like any web server, it keeps a request log for 30 days.
The [privacy policy](https://ampworth.com/privacy) has the details.

## Scope

The methods and data cover the United Kingdom only. The answers are for
understanding a system and checking a quotation. An installation still needs a
survey and a design from a certified installer. Ampworth isn't affiliated with
MCS, sells nothing and recommends no installer.

## Releasing

Releases run on a Mac, because the macOS binary is signed with `codesign` and
notarised with `notarytool`, using credentials in the keychain:

1. Once: `make notary-setup` stores a notarytool profile with your Apple ID
   and an app-specific password. The Developer ID Application certificate must
   be in the keychain too.
2. `make tag VERSION=v0.2.0` tags the version and pushes the tag.
3. `make release-check` checks everything a release needs.
4. `make release` builds every binary, signs and notarises the macOS one,
   publishes the GitHub release, and updates the cask in
   [ampworth/homebrew-tap](https://github.com/ampworth/homebrew-tap).

It uses your `gh` login for GitHub. Optional settings are in
`.env.release.example`.

## Support

Email [support@ampworth.com](mailto:support@ampworth.com) or
[open an issue](https://github.com/ampworth/cli/issues).
[Terms of use](https://ampworth.com/terms).

## Licence

[MIT](LICENSE).
