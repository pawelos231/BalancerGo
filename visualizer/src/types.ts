export type LBMode = 'NAT' | 'SNAT' | 'DSR';

export interface ClientKey {
  // Based on the Go code, the fields are not exported,
  // so I'm assuming a generic structure.
  // This can be updated if more information is available.
  [key: string]: unknown;
}

export interface ClientState {
  Key: ClientKey;
  RetrySyn: number;
  RetryData: number;
}

export interface ServerState {
  ID: string;
  Name: string;
  Address: string;
  Port: number;
  Active: boolean;
  ActiveConnections: number;
  HalfOpenConnections: number;
  LatencyP95Milli: number;
  NumberOfHandledPackets: number;
  CpuUsage: number; // 0-1
}

export interface ServerStateWithClients {
  server: ServerState;
  clients: ClientState[];
}

export interface LoadBalancerState {
  mode: LBMode;
  algorithm: string;
  servers: ServerStateWithClients[];
}
