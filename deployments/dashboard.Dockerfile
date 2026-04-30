FROM node:22-alpine AS builder
WORKDIR /app

COPY web/dashboard/package.json ./
RUN npm install

COPY web/dashboard ./
RUN npm run build

FROM nginx:1.27-alpine
COPY deployments/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist /usr/share/nginx/html
EXPOSE 80
