# sahayak-registry

A **static cartridge registry** for Sahayak. This is intentionally a *separate repo* from
the CLI — cartridges change far more often than the binary, are community/org-contributed,
and carry their own supply-chain review and signing. It is just files (no service), so it
works connected, mirrors cleanly into an air-gapped network, and is fully auditable.

## Layout
```
index.json            # the catalog: one entry per cartridge (name, url, sha256, …)
cartridges/
  k8s.json            # the cartridge files (commands + KB), served raw
  systemd.json
```

## Hosting on GitHub
Push this repo, then the index is available at its raw URL:
```
https://raw.githubusercontent.com/ZentienceLabs/sahayak-registry/main/index.json
```
Each `index.json` entry's `url` points at the raw URL of its cartridge file, and `sha256`
is the digest of that file (the CLI verifies it on install).

## Using it from the CLI
```
sahayak cartridge registry add https://raw.githubusercontent.com/ZentienceLabs/sahayak-registry/main/index.json
sahayak cartridge search redis
sahayak cartridge install k8s        # resolves via the index, verifies the checksum
```
Add multiple registries (an official one + your private/internal one) — they're peers.

## Verifying signatures (recommended)
Cartridges in this registry are signed (ed25519). Trust this publisher's key, and installs
will verify **authenticity** (who authored it) on top of the **checksum** (integrity):

```
sahayak cartridge trust add fulEJgyF8DPnFR7cKcB00whsinJc2KXHMy692WfY/3M=
sahayak cartridge install k8s        # → "signature verified"
```

Public key for `ZentienceLabs/sahayak-registry`:
```
fulEJgyF8DPnFR7cKcB00whsinJc2KXHMy692WfY/3M=
```
To require signed cartridges only, set `SAHAYAK_REQUIRE_SIGNED=1`.

## Publishing a cartridge
1. Author it: `sahayak cartridge build --name redis --kb redis-howto.md --templates redis.json`.
2. Drop the JSON in `cartridges/`.
3. Add an `index.json` entry with its raw `url` and `sha256` (`sha256sum cartridges/redis.json`).
4. (Recommended) sign it and fill `signature` — cartridges run commands, so integrity +
   signature + install-time review are the trust gate.
