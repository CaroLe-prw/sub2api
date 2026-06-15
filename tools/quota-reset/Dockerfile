FROM node:20-alpine

RUN apk add --no-cache ca-certificates postgresql-client

WORKDIR /app

COPY entrypoint.sh ./
COPY reset-subscription-daily-quota-from-announcement.mjs ./
COPY manual-reset-subscription-quota-from-announcement.mjs ./

RUN chmod +x /app/entrypoint.sh

ENTRYPOINT ["/bin/sh", "/app/entrypoint.sh"]
CMD ["schedule"]
