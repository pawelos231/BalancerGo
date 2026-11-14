# Start here

## Summary

- Describe the load balancer, backends and clients in a short text file.
- Set limits and traffic, then schedule changes in virtual time.
- **Compile and apply** checks the source and updates the planned topology.
- **Start simulation** runs the applied scenario; compiling never starts it.

## Language guide

1. [Start here](/docs/language)
2. [Syntax and units](/docs/language/syntax)
3. [Topology and capacity](/docs/language/topology)
4. [Clients and traffic](/docs/language/clients)
5. [Defaults, profiles and overrides](/docs/language/defaults)
6. [TCP settings](/docs/language/tcp)
7. [Timeline changes](/docs/language/timeline)
8. [Small experiments](/docs/language/examples)
9. [Fixing errors](/docs/language/errors)

## On this page

[First scenario](#first-scenario) · [Apply and run](#apply-and-run) · [Make one change](#make-one-change) · [Save your work](#save-your-work)

## First scenario

Open **Simulation**. The scenario editor starts in **Language** mode. Paste this
complete scenario: one load balancer, one backend and one client sending 10KB.

```lb
run 30s {
    speed 1
}

balancer lb {
}

backend api-0 {
}

clients users {
    count 1
    payload 10KB
}
```

`run 30s` sets the experiment's virtual duration. `speed 1` paces playback at
normal speed. `10KB` means 10,000 payload bytes for the client, not a rate.
The client connects through `lb` to `api-0`; an empty block keeps the defaults.

CPU processing costs zero time by default. Links still have bandwidth, travel
time and queues. See [topology and capacity](/docs/language/topology) before
interpreting a configured rate as a measured result.

## Apply and run

1. Click **Compile and apply**. If there is an error, fix the highlighted source
   and apply again. A failed apply keeps the previous applied scenario.
2. Inspect **Planned topology**. It shows configured resources and scheduled
   changes, rather than predicted traffic or throughput.
3. Click **Start simulation** to run that applied scenario.

Editing, importing or compiling source never starts a simulation. Apply pending
changes before starting. For time and speed, read
[Time and playback](/docs/language/syntax#time-and-playback) and
[how virtual time works](concept:virtual-time).

## Make one change

Replace the contents of `clients users` with this valid fragment to send three
10KB transfers, creating one client every 100ms:

```lb-fragment context=clients
count 3
payload 10KB
every 100ms
```

Apply again, inspect the plan, then start. `every` spaces client births; it does
not space the packets inside each transfer. Continue with
[clients and traffic](/docs/language/clients) or try the
[complete examples](/docs/language/examples).

## Save your work

**Export source** saves the editor text as a `.lb` file, including comments and
unfinished edits. **Import source** loads a source draft; apply it when ready.

After applying, switch to JSON and use **Export JSON** for the simulator CLI.
JSON and `.lb` are separate formats; the CLI reads the exported JSON.

For a larger editing area, use the editor's fullscreen button or **F7**. The
source and **Compile and apply** sit beside the applied JSON preview. **Esc**
leaves fullscreen. See [errors and limits](/docs/language/errors) if the source
does not apply.
