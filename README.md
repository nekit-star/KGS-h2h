# KGS-payment

Go integration for PaymentsGate H2H methods on `https://secure.easysendglobal.com`.

What is included:
- service-account JWT issuing and refresh flow
- typed HTTP client for `payin`, `payout`, `get deal`, `get trader credentials`, `update status`
- RSA webhook v3 verification for `x-api-signature`
- use case layer with terminal routing by method and traffic type (`primary`, `ftd`, `trusted`)
- in-memory merchant demo API with polling for trader credentials
- unit tests for auth, client, checksum, webhook, and use case flow

## Supported methods

- `elqr`
- `kgsphone`
- `p2p`
- `odengi`

## Configuration

Copy the example env file:

```bash
cp .env.example .env
```

Fill `BEGIN_PUBLIC_KEY` and `RSA_PRIVATE_KEY` for each terminal. The loader accepts:
- PEM with escaped `\n`
- base64-encoded PEM
- legacy `PAYMENTSGATE_PROFILE_*` variables

Current preferred env format is terminal-based:

```env
KGS_PRIMARY_TERMINAL_CODE=primary
KGS_PRIMARY_ACCOUNT_ID=...
KGS_PRIMARY_BEGIN_PUBLIC_KEY="-----BEGIN PUBLIC KEY-----\n...\n-----END PUBLIC KEY-----"
KGS_PRIMARY_RSA_PRIVATE_KEY="-----BEGIN RSA PRIVATE KEY-----\n...\n-----END RSA PRIVATE KEY-----"
```

Final provider webhooks are also mirrored by the merchant service:

```env
PAYMENTSGATE_SUCCESS_WEBHOOK_URL=https://webhook.site/b71e1b94-2e0b-43b1-8544-b9fe3325db9b
PAYMENTSGATE_FAIL_WEBHOOK_URL=https://webhook.site/b71e1b94-2e0b-43b1-8544-b9fe3325db9b
```

Behavior:
- `completed` -> success webhook URL
- `canceled` and `expired` -> fail webhook URL

The default terminal mapping is already wired to the credential sets you sent:
- `elqr` -> `kgs_primary`
- `kgsphone trusted` -> `kgs_phone_trusted`
- `p2p ftd/trusted` -> `kgs_p2p_ftd` / `kgs_p2p_trusted`
- `odengi ftd/trusted` -> `kgs_odengi_ftd` / `kgs_odengi_trusted`

If your real routing differs, change the `KGS_METHOD_*_TERMINAL` variables without touching code.

## Demo HTTP API

Run the merchant demo:

```bash
go run ./cmd/merchant -env .env
```

Endpoints:
- `POST /payins`
- `POST /payouts`
- `GET /deals/{deal_id}`
- `PATCH /deals/{deal_id}/status`
- `POST /webhooks/paymentsgate`

Example payin:

```json
{
  "method": "elqr",
  "traffic_type": "primary",
  "amount": 61,
  "currency": "KGS",
  "invoice_id": "inv-1001",
  "client_id": "client-42",
  "elqr_banks": ["mbank", "bakai", "kicb"]
}
```

Example payout:

```json
{
  "method": "odengi",
  "traffic_type": "trusted",
  "amount": 10,
  "currency_to": "KGS",
  "invoice_id": "inv-2001",
  "client_id": "client-42",
  "recipient": {
    "account_number": "12345678901",
    "account_owner": "AIBEK",
    "type": "phone"
  }
}
```

## Tests

```bash
GOCACHE=../.gocache go test ./...
```

## Notes

- Payin finalization window is modeled as `10m`.
- Payout finalization window is modeled as `12h`.
- The service polls `/deals/credentials/{id}` until credentials appear or the configured wait timeout expires.
- Webhook verification follows the v3 RSA checksum flow from PaymentsGate docs.

## Sources

- [Base docs](https://paymentsgate.readme.io)
- [Webhook signature types](https://paymentsgate.readme.io/docs/webhook-signature-types)
- [Deal statuses](https://paymentsgate.readme.io/docs/understanding-deal-statuses)
- [ELQR](https://paymentsgate.readme.io/docs/elqr-in-new)
- [KGS phone](https://paymentsgate.readme.io/docs/kgsphone)
# KGS-h2h
