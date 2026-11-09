# Support

- Usage and deployment questions: use GitHub Discussions after enabling it for the repository.
- Reproducible bugs: use the bug report template.
- Feature proposals: use the feature request template.
- Security vulnerabilities: follow [SECURITY.md](SECURITY.md) and do not file a public issue.

Before asking for help:

1. Run `wvc version` and `wvc doctor`.
2. Run `wvc healthcheck`.
3. Check `journalctl -u webvirtcloud-go -n 200 --no-pager` or `docker compose logs --tail=200 app`.
4. Remove secrets, private hostnames, internal IPs, tokens, and database credentials from logs before posting.

Community support is provided on a best-effort basis and does not include an SLA.
