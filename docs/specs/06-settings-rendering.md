# 06 — Settings rendering

Sources: `api/v1alpha1/palworldsettings_types.go` (the generated type),
`internal/settings/render.go` (the renderer), `internal/resources/configmap.go`
(wiring + Engine.ini + hash).

## The settings type

`PalworldServerSettings` has one field per user-facing `PalWorldSettings.ini`
`OptionSettings` key. Of the 119 shipped keys, **111 are user fields**; 8 are
operator-managed and excluded from the type: `AdminPassword`, `ServerPassword`,
`PublicIP`, `PublicPort`, `RCONEnabled`, `RCONPort`, `RESTAPIEnabled`,
`RESTAPIPort` (these are injected at render time). The type was generated from an
authoritative Palworld 1.0 key catalog; it is now maintained directly. Enum
values live in named types (`Difficulty`, `RandomizerType`, `DeathPenalty`,
`LogFormatType`, `CrossplayPlatform`).

Each field carries a struct tag that is the **single source of truth** for
rendering:

```
pal:"<IniKey>,<kind>,<quote>"
```

- `<IniKey>` — the exact INI key (preserves canonical spelling, e.g.
  `PlayerStomachDecreaceRate`, mixed `HP`/`Hp` casing, underscores).
- `<kind>` ∈ `bool | int | float | enum | string | platforms`.
- `<quote>` ∈ `q` (quoted) | `n` (unquoted); only consulted for `string`.

The JSON field name is the key with the leading Hungarian `b` stripped and the
first letter lowercased (e.g. `bIsMultiplay` → `isMultiplay`); when stripping
would collide, the `b` is kept (`bAdditionalDropItemWhenPlayerKillingInPvPMode`
→ `bAdditionalDropItemWhenPlayerKillingInPvPMode`).

## Render (`settings.Render`)

Produces:

```
[/Script/Pal.PalGameWorldSettings]
OptionSettings=(Key1=Val1,Key2=Val2,...)
```

`renderStruct` reflects over the type in declaration order and formats each field
by `<kind>`:

| kind | format |
| ---- | ------ |
| `bool` | `True` / `False` |
| `int` | decimal |
| `float` | `%.6f` (6 decimals, e.g. `1.000000`) |
| `enum` | bare token (unquoted) |
| `string` | `"…"` if `q`, else bare; value passed through `escapeIniString` |
| `platforms` | `(A,B,C)` (unquoted parenthesized list) |

`escapeIniString` removes `"`, `\n`, and `\r` (embedded quotes would break the
single-line tuple; the webhook also rejects them, spec 09).

`injectedPairs` are appended after the struct fields, in this order:

```
AdminPassword="__PALWORLD_ADMIN_PASSWORD__"
ServerPassword="__PALWORLD_SERVER_PASSWORD__"
PublicIP="<publicIP>"
RCONEnabled=True|False
RCONPort=<n>
RESTAPIEnabled=True|False
RESTAPIPort=<n>
PublicPort=<n>            # only when > 0
```

The two `__PALWORLD_*_PASSWORD__` tokens are placeholders replaced at container
start from Secret-backed env (spec 07), so **no plaintext password is ever
written to the ConfigMap**.

## Wiring (`resources.RenderSettings`)

Builds `InjectOptions` with `RCONEnabled=true`/`RCONPort=25575`,
`RESTAPIEnabled=true`/`RESTAPIPort=8212`, `PublicIP` from
`networking.publicIP`, and `PublicPort` from `networking.publicPort` (falling
back to `GamePort`). RCON and the REST API are therefore **always enabled** so
the operator can perform day-2 operations.

## Engine.ini

`renderEngineINI` turns `spec.engineSettings` (`map[string]string` keyed
`Section/Key`) into a deterministic INI. The section/key split is on the **last**
`/` (Unreal section names contain slashes, e.g.
`/Script/OnlineSubsystemUtils.IpNetDriver`); keys with no `/` default to section
`/Script/Engine.Engine`. Sections and lines are sorted for stable output.

## Config hash

`SettingsHash` = `settings.Hash(rendered_ini + "\x00" + rendered_engine)` where
`Hash` is the first 16 hex chars of the SHA-256. It is stamped on the pod
template as annotation `palworld.twodcube.io/settings-hash`, so any settings or
engine change rolls the StatefulSet pod (spec 02).
