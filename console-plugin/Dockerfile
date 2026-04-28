FROM node:22-slim AS build
ENV COREPACK_ENABLE_DOWNLOAD_PROMPT=0
RUN corepack enable

COPY . /usr/src/app
WORKDIR /usr/src/app
RUN yarn install --immutable && yarn build

FROM registry.access.redhat.com/ubi9/nginx-120:latest

COPY --from=build /usr/src/app/dist /opt/app-root/src
USER 1001

ENTRYPOINT ["nginx", "-g", "daemon off;"]
