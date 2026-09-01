# Crypto Price Alert

Backend service for scheduled crypto price notifications.

The implementation plan is documented in `docs/plans/crypto-price-alert-mvp-implementation-plan.md`.

## Local Docker setup

Copy `.env.example` to `.env`, fill notification secrets as needed, then run:

```bash
docker compose config
docker compose up --build
```

The application is exposed at `http://localhost:8080` and PostgreSQL data is stored in the `postgres_data` volume.
