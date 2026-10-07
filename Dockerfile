FROM debian:13-slim
WORKDIR /app
COPY ticketlab /app/ticketlab
RUN mkdir -p /data && chown 10001:10001 /data
USER 10001:10001

EXPOSE 8080
ENTRYPOINT ["/app/ticketlab"]
CMD ["serve", "--listen", "0.0.0.0:8080", "--data-dir", "/data", "--instance", "docker-lab"]
