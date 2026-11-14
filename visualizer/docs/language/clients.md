# Clients and traffic

## Summary

A `clients` group schedules TCP clients. `count` is its lifetime birth budget, `every` spaces their births, and `payload` is the total application data each client sends. A `client users-1` block adjusts one existing slot.

## On this page

- [Schedule clients](#schedule-clients)
- [Lifetime client budget](#lifetime-client-budget)
- [Payload and TCP segments](#payload-and-tcp-segments)
- [Override one client](#override-one-client)
- [Change a running group](#change-a-running-group)

## Schedule clients

Put these fields inside a `clients users` block. This fragment replaces that block's contents in a scenario with a run, balancer and backend:

```lb-fragment context=clients
count 3
start 1s
every 2s
payload 10KB
segment-gap 5ms
```

This schedules `users-0`, `users-1` and `users-2` at virtual times 1s, 3s and 5s. Each sends 10,000 payload bytes. `segment-gap 5ms` spaces original TCP segment sends within each client's transfer; it does not space client births. TCP windows, ACKs, queues and loss can delay sending further.

| Field | Meaning | Default |
| --- | --- | --- |
| `count` | Total lifetime slots the group may create | `1` |
| `start` | Absolute virtual time of its first birth | `0ns` initially; insertion time for an added group |
| `every` | Delay between client births | `0ns`, births together |
| `payload` | Total payload bytes per client | Inherit workload defaults |
| `packets` | Number of 1024-byte payload units when payload is `-1` | Inherit workload defaults |
| `segment-gap` | Delay between original TCP segment sends | Inherit workload defaults |

`count`, `start`, `every` and `segment-gap` must be nonnegative. An existing group's `start` stays fixed. Births beyond the run's horizon remain unstarted; configured clients need not all be created or finish.

[How client lifetimes work](concept:client-lifecycle) · [How virtual time works](concept:virtual-time)

## Lifetime client budget

`count 10` means at most ten clients over this group's lifetime. It does not maintain ten active connections, replace closed clients or add ten more clients each time it is set.

If six clients have already been born, setting `count 8` permits two more. Setting `count 4` stops further births and keeps all six existing clients. A later increase to `count 8` resumes with slot 6; it does not restart at slot 0.

Slots have historical identities: `users-1` is always index 1, with runtime ID `client/users/1`. Reducing count or removing the group never frees those identities. A timeline setter may still adjust a historically reserved slot after a count reduction. See [timeline commands](/docs/language/timeline#commands-and-what-they-preserve).

The whole document may reserve at most 16,777,215 client slots. The compiler sums each group's highest count across all states, including removed groups. Two successive groups of ten million clients exceed the limit even if their births never overlap.

## Payload and TCP segments

`payload 10KB` means 10,000 application bytes **per client**, not per segment or per group. Three such clients offer 30,000 payload bytes in total; TCP headers and retransmissions add network traffic.

This complete scenario uses the packet-count fallback with a different MSS:

```lb
run 30s {
    speed 0
}
tcp {
    mss 1200B
}
balancer lb {
}
backend api-0 {
}
clients users {
    count 1
    payload -1
    packets 3
}
```

The client sends `3 × 1024 = 3072` payload bytes. MSS 1200B splits that data into 1200B, 1200B and 672B segments. `packets` is a legacy workload size unit; its multiplier stays 1024 regardless of MSS. An explicit nonnegative `payload` takes precedence over `packets`.

`payload 0B` is valid: the client still establishes TCP, completes an empty transfer and follows the closing behavior. It is useful for connection-only workloads. Omitting both stream size fields inherits `payload -1` and `packets 10000`, or the values supplied by your `defaults` block.

[How MSS works](concept:mss) · [How TCP closing works](concept:closing)

## Override one client

Add this top-level declaration alongside `clients users` with `count 3`:

```lb-fragment context=top-level
client users-1 {
    payload 0B
    link 1MB
    from-lb {
        travel 20ms
    }
}
```

There are still three clients. Slot 1 gets an empty transfer and its own link settings; slots 0 and 2 keep their group settings. This is a sparse override, not an extra client declaration.

An initial override must identify a slot inside the group's declared count and may appear before the group. Only one initial override may target each slot. It may set stream fields, network fields, directions and an existing profile. It cannot set `count`, `start`, `every`, CPU settings or identity.

Individual stream fields inherit the group's resolved stream; individual network settings apply after the group's resolved links. A nullable leaf such as `payload null` removes that individual patch and inherits the group. See [inheritance](/docs/language/defaults#inheritance-order).

## Change a running group

This complete scenario starts `users` at 0s, then changes the group between births:

```lb
run 30s {
    speed 0
}
balancer lb {
}
backend api-0 {
}
clients users {
    count 3
    every 10s
    payload 10KB
}
at 5s {
    set clients users {
        payload 20KB
        link 1MB
    }
}
at 15s {
    stop arrivals users
}
```

`users-0`, born at 0s, keeps its 10KB stream. `users-1`, born at 10s, gets 20KB. The network change at 5s also updates `users-0`'s existing links. Stopping arrivals at 15s prevents slot 2's birth at 20s and preserves both existing connections.

Streams freeze at birth: later group or individual changes to `payload`, `packets` or `segment-gap` affect future births only. Network changes also reach already created clients. Changing `every` replans the next birth from the last birth time, without scheduling it before the change timestamp.

Continue with [defaults and profiles](/docs/language/defaults) or [timeline rules](/docs/language/timeline#timeline-rules).
