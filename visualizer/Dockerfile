FROM node:24-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY . .
ARG VITE_WS_URL=ws://localhost:8081/ws
RUN VITE_WS_URL="$VITE_WS_URL" npm run build

FROM node:24-alpine
RUN npm install --global local-web-server@5.4.0 --no-audit --no-fund
WORKDIR /www
COPY --from=build /app/dist ./
USER node
EXPOSE 5173
CMD ["ws", "--hostname", "0.0.0.0", "--port", "5173", "--spa", "index.html"]
