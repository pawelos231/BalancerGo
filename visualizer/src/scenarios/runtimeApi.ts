import { ScenarioApi, resolveApiBase } from './api';

export const websocketUrl = import.meta.env.VITE_WS_URL || 'ws://localhost:8080/ws';

export const scenarioApi = new ScenarioApi(
  resolveApiBase(import.meta.env.VITE_API_URL, websocketUrl),
);
