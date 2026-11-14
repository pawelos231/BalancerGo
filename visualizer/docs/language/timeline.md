# Timeline changes

## Summary

An `at` block prepares one atomic configuration change at a virtual timestamp. Use it to add capacity, change traffic or model backend failure during a run. Created clients keep running; they may detect a failed connection through TCP timeouts.

## On this page

- [First timeline](#first-timeline)
- [Timeline rules](#timeline-rules)
- [Commands and what they preserve](#commands-and-what-they-preserve)
- [Atomic changes and conflicts](#atomic-changes-and-conflicts)
- [Fixed and mutable settings](#fixed-and-mutable-settings)
- [Backend availability](#backend-availability)

## First timeline

This complete scenario adds a burst, lowers the LB's forwarding budget and then stops new arrivals:

```lb
run 30s {
    speed 0
}
balancer lb {
    egress 100MB
}
backend api-0 {
}
clients users {
    count 3
    payload 10KB
}
at 5s {
    add clients burst {
        count 10
        every 1s
        payload 20KB
    }
}
at 8s {
    set balancer egress 10MB
}
at 10.5s {
    stop arrivals burst
}
```

The burst starts at 5s because an added group's omitted `start` defaults to its insertion time. Six burst clients are born at 5s through 10s. Stopping arrivals before the next birth at 11s keeps those clients running.

[How virtual time works](concept:virtual-time) · [How client lifetimes work](concept:client-lifecycle)

## Timeline rules

- Write every initial declaration before the first `at`, including profiles and individual client overrides.
- Timestamps must satisfy `0 < timestamp < run duration` and increase strictly in source order.
- Equal converted times are duplicates: `1s` and `1000ms` cannot label separate blocks. Put their commands in one block.
- Each block needs at least one command. All states are compiled and validated before a run starts; a failed block rejects the document.
- An added group's explicit `start` must be at or after its insertion time. An existing group's start cannot change.

Each block starts from the preceding configured state. Unchanged settings carry forward. The simulator applies the resulting complete state atomically at the timestamp; packets do not observe partially applied commands.

## Commands and what they preserve

| Command form | Changes | Preserves |
| --- | --- | --- |
| `set balancer egress 10MB` | A mutable LB field | LB identity and existing connections |
| `set backend api-0 link 5MB` | An existing backend's settings | Identity, address, port and existing mappings |
| `set clients users count 10` | A group's lifetime birth budget or other mutable fields | Already born clients and historical slots |
| `set client users-1 payload 20KB` | One reserved slot's sparse override | The group's count and the client's identity |
| `add backend api-1 { }` | Adds one backend | Existing resources |
| `add backends extra { count 2 }` | Adds `extra-0` and `extra-1` | Independent identity for each backend |
| `add clients burst { count 10 }` | Adds a new generator | Existing generators and connections |
| `down backend api-0` / `up backend api-0` | Physical availability only | Administrative state and existing identities |
| `drain backend api-0` | Administrative admission becomes `drain` | Physical availability and existing mappings |
| `stop arrivals users` | Sets count to zero; stops future births | Group, settings, historical overrides and existing clients |
| `remove clients users` | Removes the generator from later configuration and planned topology | Created clients and historical identity reservations |

`set` also accepts a block for several fields or nested settings. `set balancer` identifies the single LB without repeating its name.

`stop arrivals` keeps the group available for later setters. To resume births, raise its count above the number already born; count is a [lifetime budget](/docs/language/clients#lifetime-client-budget). `remove clients` makes later references invalid, and the identity cannot be reused. Neither command kills existing TCP connections.

Stream settings (`payload`, `packets`, `segment-gap`) are captured when each client is born. Their changes affect future births; network changes also update already created clients. See [changing a running group](/docs/language/clients#change-a-running-group).

## Atomic changes and conflicts

Commands are read in source order during compilation. A resource must exist before a command references it. Add a backend before setting it:

```lb-fragment context=timeline
at 5s {
    add backend api-1 {
        link 5MB
    }
    set backend api-1 link 10MB
    set backend api-1 host {
        packet-time 20us
    }
}
```

The `add` block is an initializer, so the following setter may replace its 5MB rate with 10MB. Later setters may change other leaves, as the host setter does here. The final state is what packets see.

Within an `at`, writing the same native leaf twice is an error. This invalid timeline fragment demonstrates an overlap:

```lb-invalid context=timeline error="overlaps an earlier value"
at 5s {
    set backend api-0 link 4MB
    set backend api-0 to-lb {
        link 1MB
    }
}
```

Shared `link` writes both directions, so the second setter writes `to-lb.link` again. Set `to-lb` and `from-lb` separately to give them different rates. This differs from an initial declaration, where directional values intentionally override shared shorthand.

The same conflict rule covers aliases: `down` and `set available` write one field, while `stop arrivals` and `set count` write one field. An `add` initializer is exempt, but two subsequent setters of the same leaf still conflict.

## Fixed and mutable settings

| Fixed after declaration | Mutable inside `at` |
| --- | --- |
| Run duration, seed, speed, snapshots and event limit | LB algorithm, ingress, egress and mapping idle timeouts |
| TCP settings | LB/backend host cores, packet time and CPU queue |
| Global defaults and profile definitions | Endpoint network fields and selection of an existing profile |
| LB ID and resource roles | Backend display name, physical availability and administrative state |
| Backend ID, address and port | Group count, `every`, stream settings and sparse client overrides |
| Retained group ID and `start`; historical identity reservations | New backends and client groups through `add` |

Profiles are fixed, but endpoints may switch to an already declared profile. A new resource's initializer can set its identity and start settings. Backend removal, mutable TCP, client CPU and direct observed-health setters are unsupported.

## Backend availability

Physical availability and administrative admission are separate controls. This fragment shows a failure, return while draining and explicit readmission:

```lb-fragment context=timeline
at 5s {
    drain backend api-0
    down backend api-0
}
at 10s {
    up backend api-0
}
at 15s {
    set backend api-0 admin active
}
```

At 10s the backend is physically up but remains in drain: `up` does not undrain it. At 15s `admin active` admits new mappings again, subject to the LB's runtime view of backend health. Drain keeps existing mappings; it is not a request to move their TCP connections.

A backend going down does not instantly update the LB's observed health, notify clients or reroute established TCP. Recovery follows the simulator's health and transport behavior. Clients may retransmit TCP packets, but the simulator does not add application retries or replay failed requests on another backend.

[How backend lifecycle works](concept:backend-lifecycle) · [How TCP retransmission timing works](concept:rto)

Continue with [complete experiments](/docs/language/examples) or [errors and fixes](/docs/language/errors).
