# 文件作用说明：构建一个同时运行Go API和Next.js官网工作台的GoodChat应用镜像，不包含数据库或Redis服务。
FROM golang:1.26-alpine AS backend-build
WORKDIR /build
ENV GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN go build -trimpath -o /out/goodchat ./cmd/server && go build -trimpath -o /out/migrate ./cmd/migrate && go build -trimpath -o /out/bootstrap ./cmd/bootstrap

FROM node:22-alpine AS frontend-build
WORKDIR /build
ENV NEXT_TELEMETRY_DISABLED=1
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --registry=https://registry.npmmirror.com
COPY frontend/ ./
ARG NEXT_PUBLIC_SITE_URL=http://127.0.0.1:13000
ENV NEXT_PUBLIC_SITE_URL=${NEXT_PUBLIC_SITE_URL} BACKEND_URL=http://127.0.0.1:18080
RUN npm run build

FROM node:22-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 PORT=3000 SUPPORT_ADDR=127.0.0.1:18080 SUPPORT_DATA_DIR=/data BACKEND_URL=http://127.0.0.1:18080
COPY --from=backend-build /out/goodchat /out/migrate /out/bootstrap /app/
COPY --from=frontend-build /build/.next/standalone /app/frontend/
COPY --from=frontend-build /build/.next/static /app/frontend/.next/static/
COPY --from=frontend-build /build/public /app/frontend/public/
COPY deploy/ /app/deploy/
# 图片优化缓存供运行时非特权用户写入，避免只读源码目录导致Next.js图片请求失败。
RUN mkdir -p /app/frontend/.next/cache && chmod 1777 /app/frontend/.next/cache
EXPOSE 3000 18081
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=3 CMD node -e "fetch('http://127.0.0.1:18080/healthz').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
CMD ["node","/app/deploy/start.mjs"]
