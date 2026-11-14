# Small experiments

## Summary

These six complete configurations build from one transfer to timed changes.
Paste any one into the Language editor, compile and apply, then start the
simulation. Each uses few clients, small payloads and `speed 0` for a quick run.

## On this page

[One transfer](#one-transfer) ·
[Different backend links](#different-backend-links) ·
[An LB forwarding bottleneck](#an-lb-forwarding-bottleneck) ·
[CPU processing](#cpu-processing) · [A burst](#a-burst) ·
[Backend failure and return](#backend-failure-and-return)

These are experiments, not calibrated hardware profiles. Compare measured
results after changing one value; a configuration does not guarantee drops,
completed transfers or a particular routing outcome. Planned topology shows
configuration, while a running simulation produces observations.

## One transfer

Start with one client sending 10KB through one LB to one backend. Watch the
connection lifecycle and transferred bytes. All network and TCP settings use
the [language defaults](/docs/language/defaults).

```lb
run 4s {
    speed 0
}
balancer lb {}
backend api-0 {}
clients users {
    count 1
    payload 10KB
}
```

Try `payload 20KB` next. `payload` is bytes per client, not the total for the
group. [How a client lifecycle works](concept:client-lifecycle).

## Different backend links

Two backends now have different link rates. Round robin selects among eligible
backends; the declared rates remain capacity limits rather than promised
throughput. Watch each backend's transferred bytes and network queue.

```lb
run 5s {
    speed 0
}
defaults {
    link 10MB
}
balancer lb {
    algo rr
}
backend fast-0 {
    link 10MB
}
backend slow-0 {
    link 100KB
}
clients users {
    count 4
    payload 40KB
    every 20ms
}
```

Try raising `slow-0` to `1MB` and compare its observed transfer rate.
`link` sets both directions; use `to-lb` and `from-lb` to separate them.
[How links work](concept:links).

## An LB forwarding bottleneck

The endpoint links remain faster than the LB's shared 100KB/s forwarding
budget. Watch LB forwarding throughput and output queues. More clients share
that same budget; `egress` does not allocate a separate rate per client.

```lb
run 6s {
    speed 0
}
defaults {
    link 10MB
}
balancer lb {
    ingress 10MB
    egress 100KB
}
backend api-0 {}
clients users {
    count 4
    payload 40KB
}
```

Try `egress 1MB` next. Ingress is the shared physical receiver; egress is a
shared forwarding limit over the output links.
[How ingress works](concept:ingress) · [How egress works](concept:egress).

## CPU processing

The LB and backend each process packets with one modeled worker and finite
waiting queues. Watch CPU utilization, waiting work and transfer timing.
`host.queue` counts CPU work, separately from network packet queues.

```lb
run 6s {
    speed 0
}
defaults {
    link 10MB
}
balancer lb {
    host {
        cores 1
        packet-time 1ms
        queue 32
    }
}
backend api-0 {
    host {
        cores 1
        packet-time 2ms
        queue 32
    }
}
clients users {
    count 4
    payload 30KB
}
```

Try backend `cores 2`, or set its `packet-time` to `0ns` to bypass modeled CPU
processing. Finite queues make dropping possible when arrivals exceed capacity;
this small workload does not promise a drop. [How CPU processing works](concept:cpu).

## A burst

An initial transfer is followed by a small added group at 1s. Its clients are
born 250ms apart. At 2s, `stop arrivals` prevents later births while existing
connections continue. Watch created client counts and active connections.

```lb
run 5s {
    speed 0
}
balancer lb {}
backend api-0 {}
clients users {
    count 1
    payload 10KB
}
at 1s {
    add clients burst {
        count 6
        payload 20KB
        every 250ms
    }
}
at 2s {
    stop arrivals burst
}
```

An added group without `start` begins at its insertion time. This burst creates
four clients, at 1s, 1.25s, 1.5s and 1.75s. Configuration changes run before
client births at the same timestamp, so the stop at 2s cancels the next birth.
Try moving the stop to `2500ms`. See [timeline commands](/docs/language/timeline)
and [how virtual time works](concept:virtual-time).

## Backend failure and return

One backend becomes physically unavailable at 1s and returns at 3s. Staggered
clients keep the experiment active around those changes. Watch availability,
LB-observed health, connection progress and retries separately.

```lb
run 6s {
    speed 0
}
balancer lb {
    algo rr
}
backend api-0 {}
backend api-1 {}
clients users {
    count 6
    payload 20KB
    every 700ms
}
at 1s {
    down backend api-0
}
at 3s {
    up backend api-0
}
```

Physical failure does not immediately update the LB's observed health or move
an established TCP connection. Return does not guarantee immediate reuse or
completion before the horizon. Try changing the return time and compare the
observations. [How backend lifecycle works](concept:backend-lifecycle).

For individual fields, continue with [topology](/docs/language/topology),
[clients](/docs/language/clients) or [TCP settings](/docs/language/tcp).
