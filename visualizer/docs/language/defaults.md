# Defaults, profiles and overrides

## Summary

Set common network and workload values in `defaults`, reuse network patches with `profile`, then override the fields that differ. Inheritance works field by field; `null` inherits and an allowed zero is an explicit value.

## On this page

- [Set common defaults](#set-common-defaults)
- [Inheritance order](#inheritance-order)
- [Profiles](#profiles)
- [Null and zero](#null-and-zero)
- [CPU and queue settings](#cpu-and-queue-settings)
- [Built-in defaults](#built-in-defaults)

## Set common defaults

Add this top-level block before any `at` blocks:

```lb-fragment context=top-level
defaults {
    link 20MB
    travel 2ms
    queue 128
    loss 0%
    payload 10KB
    segment-gap 1ms
}
```

Endpoint links inherit 20,000,000 bytes/s, 2ms of travel and 128 waiting packet slots. Client streams inherit 10,000 payload bytes and 1ms spacing between original segments. `defaults` may contain the network fields below plus `payload`, `packets` and `segment-gap`.

The bandwidth also supplies the initial LB `ingress` and `egress` rates unless explicitly set. Other ingress fields keep their separate built-in defaults: for example, `defaults travel 2ms` does not change ingress travel from 0ns.

| Network field | Meaning |
| --- | --- |
| `link` | Positive bytes per second; `1MB` is 1,000,000 bytes/s |
| `travel` | Nonnegative travel time after serialization |
| `queue` | Nonnegative waiting packet capacity |
| `loss` | Random loss, `0%` through `100%` |
| `queue-policy` | `tail-drop`, `threshold` or `default` |
| `ecn-threshold` | Waiting occupancy threshold, `0%` through `100%` |
| `signal-probability` | Probability of signaling above that threshold |

`default` selects the native default queue policy, currently tail drop. With `threshold`, eligible ECN packets can be marked and packets without ECN capability can be dropped early. Full queues still drop arrivals. See [TCP and ECN](/docs/language/tcp) and [unit syntax](/docs/language/syntax).

[How links work](concept:links) · [How ingress works](concept:ingress) · [How egress works](concept:egress)

## Inheritance order

Resolve each network field in this order, with later supplied values winning:

1. Built-in values, changed by `defaults`.
2. The endpoint's selected `profile`.
3. Shared endpoint settings, such as `link` or `travel`.
4. Directional `to-lb` or `from-lb` settings.
5. A client's individual override, applied to its resolved group links: its profile first, then shared and directional settings.

This complete example gives each layer a visible effect:

```lb
run 30s {
    speed 0
}
defaults {
    link 20MB
    travel 2ms
}
profile mobile {
    link 5MB
    travel 30ms
}
balancer lb {
}
backend api-0 {
}
clients users use mobile {
    count 3
    payload 10KB
    link 4MB
    from-lb {
        link 2MB
    }
}
client users-1 {
    to-lb {
        link 1MB
    }
}
```

| Endpoint | `to-lb` rate | `from-lb` rate | Travel, both directions |
| --- | --- | --- | --- |
| Backend `api-0` | 20MB/s = 160,000,000 bit/s | 20MB/s = 160,000,000 bit/s | 2ms |
| Clients `users-0`, `users-2` | 4MB/s = 32,000,000 bit/s | 2MB/s = 16,000,000 bit/s | 30ms |
| Client `users-1` | 1MB/s = 8,000,000 bit/s | 2MB/s = 16,000,000 bit/s | 30ms |

The profile replaces the default rate with 5MB/s. The group's shared `link 4MB` replaces that rate in both directions, then `from-lb` replaces one direction. Slot 1 changes only its `to-lb` rate. These are configured limits; they do not promise those transfer speeds.

In initial declarations and `add` initializers, directional values win over shared shorthand regardless of their textual order. Inside timeline mutations, overlapping writes are errors: `set backend api-0 link 4MB` and `set backend api-0 to-lb { link 1MB }` in the same `at` both write the same directional leaf. Set the two directions separately instead. See [atomic changes](/docs/language/timeline#atomic-changes-and-conflicts).

## Profiles

Profiles contain network settings only. They cannot contain workload, CPU or directional blocks, select another profile, or change during the run. Every profile is validated, including unused ones.

Select one with `clients users use mobile` or a `use mobile` field inside the block. Use one form per declaration; supplying both is a duplicate. Definitions may appear after the resources that use them, but must precede the first `at` block.

An endpoint can switch to another existing profile inside `at`; its explicit endpoint patches still win. An empty profile, such as `profile plain { }`, contributes no patches and can replace a previous profile selection. For a client override, that restores the resolved group base; for a backend or group, it restores the global defaults beneath endpoint patches. `use null` is unsupported.

## Null and zero

Only nullable scalar leaves accept `null`. On a network patch, it removes that leaf's override so the profile or base network supplies it. On an individual stream it inherits the group; on a group stream it inherits fixed workload defaults. CPU, TCP, mapping timeout and backend availability/admin leaves use their model defaults.

This complete scenario removes a previous network override at 5s:

```lb
run 30s {
    speed 0
}
defaults {
    link 20MB
    travel 2ms
}
profile mobile {
    link 5MB
}
balancer lb {
}
backend api-0 use mobile {
    link 4MB
    travel 20ms
    loss 5%
}
clients users {
    count 3
    payload 10KB
}
at 5s {
    set backend api-0 {
        travel null
        loss 0%
        to-lb {
            link null
        }
    }
}
```

At 5s, both backend directions inherit 2ms travel and explicitly use 0% loss. `to-lb` inherits the profile's 5MB/s (40,000,000 bit/s), while `from-lb` keeps 4MB/s (32,000,000 bit/s). Shared `link` writes two directional patches, so clearing one direction inherits its profile or base rather than restoring the shared shorthand as another layer.

`null` does not preserve the previous 20ms travel value. Omitting a field from a timeline setter does preserve its previous patch.

Zero must be allowed by that field: `travel 0ns`, `loss 0%`, `queue 0` and `payload 0B` are useful explicit values. `link 0B` is invalid because bandwidth must be positive. Whole blocks, complete network defaults and LB ingress settings do not accept null; use a nullable leaf in a supported patch.

## CPU and queue settings

Put this fragment inside a `balancer lb` block to model two CPU workers:

```lb-fragment context=balancer
host {
    cores 2
    packet-time 10us
    queue 0
}
```

Two workers at 10us per packet give an ideal CPU service limit of `2 / 0.00001 = 200,000` packet tasks/s before other bottlenecks. `host.queue 0` allows no waiting work; an idle worker can still accept a packet. Cores must be 1–1024 and CPU queue capacity must be 0–1,000,000.

`packet-time 0ns`, the default, bypasses CPU processing. It does not model a zero-capacity CPU. A network `queue 0` similarly permits immediate transmission when capacity is free but no waiting packets. CPU queues and network queues are separate; active work, active transmission and packet travel do not occupy waiting slots.

[How CPU processing works](concept:cpu) · [How queues work](concept:queues)

## Built-in defaults

These version-1 values are independent of server CLI flags:

| Setting | Value |
| --- | --- |
| Network rate / travel / queue / loss | 125MB/s / 1ms / 1024 waiting packets / 0% |
| Queue policy / ECN threshold / signal probability | tail drop / 0% / 100% |
| LB ingress travel / algorithm | 0ns / `rr` |
| LB ingress / egress rates | Network default rate |
| Host cores / packet time / queue | 1 / 0ns bypass / 1024 |
| Workload payload / packets / segment gap | `-1` / 10000 / 0ns |
| Client count / start / birth gap | 1 / 0ns / 0ns |
| Backend physical availability / admin | `true` / `active` |
| Seed / speed / snapshots / event limit | 1 / 1 / 100ms real time / 5,000,000,000 |
| Mapping idle: half-open / established / closing / reset | 5s / 50s / 120s / 10s |

See [TCP defaults](/docs/language/tcp) for transport settings and [timeline mutability](/docs/language/timeline#fixed-and-mutable-settings) for what can change.
