FROM scratch
COPY obsidian-sync /usr/local/bin/obsidian-sync
COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
EXPOSE 80
ENTRYPOINT ["/usr/local/bin/obsidian-sync"]
