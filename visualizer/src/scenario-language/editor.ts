// A small editable example, independent of whichever JSON preset is currently applied.
export const initialLanguageSource = `run 180s seed 1 speed 1
defaults link 1GB travel 1ms queue 1024
tcp congestion reno mss 1460B

balancer lb {
    ingress 1GB
    egress 1GB
}

backends api {
    count 2
    link 250MB
}

clients regular {
    count 8
    payload 1MB
    every 10ms
    link 10MB
}
`;
