# DCL data: may the production PAA and CD-signer certificates be embedded? (read 2026-10-17)

Owner question from concept v3 §A.2 ("Zu klären vor dem Commit"). Read by
the owner-side agent (Fable); sources fetched 2026-10-17.

## What the CSA documents say

- **Matter PKI Certificate Policy** (csa-iot.org/wp-content/uploads/2024/02/pki-certificate-policy-2025.pdf,
  version f0ca48e914 2024-08-26, licensed **CC BY 4.0**, p. 1):
  - §2.1 "PAA Certificates will be published in the Distributed Compliance
    Ledger (DCL) as a repository." (p. 16)
  - §2.2 "This CP and CA Certificates SHALL be publicly available"; Table 5
    classifies **PAA Certificates: Public**, PAI Certificates: (Optional)
    Public, CPSs: Confidential. (p. 16)
  - §2.3 "PAA Certificates SHALL be made publicly available (through the
    DCL) at the time of submission of a request for inclusion in the
    Matter PKI." (p. 16)
  - §4.4.2 "PAA Certificates SHALL be published in a publicly available
    repository." (p. 22)
  - §1.4.5 "The Relying Party MAY be any entity that validates the binding
    of a public key … The Relying Party is responsible for deciding whether
    or how to check the validity of the Certificate by checking the
    appropriate Certificate status information." (p. 7)
  - §4.5.2 Relying Parties "are solely responsible for deciding whether to
    rely on the information in a Certificate"; the Alliance disclaims
    warranties and limits liability. (p. 22)
- **CSA DCL page** (csa-iot.org/certification/distributed-compliance-ledger/):
  "The DCL is permissionless for read operations and requires authorized
  accounts for writing operations"; participants "publish public
  information about a given device or class of devices".
- **DCL repository** (github.com/zigbee-alliance/distributed-compliance-ledger,
  Apache-2.0): "Anyone can read from the ledger (that's why it's public)."
- **CSA website Terms of Use** (csa-iot.org/terms-conditions/): govern the
  websites and the Specifications ("no material from the Site may be
  copied … without prior written consent"); they do not mention the DCL,
  certificates or ledger data. Ledger data is served by observer nodes,
  not by the website.
- **DCL whitepaper** (September 2022, p. 4): the DCL is "a platform where
  a list of trusted roots … can be maintained"; p. 10: "the DCL can
  distribute authorized X.509 Certificates (root and non-root)". Its
  notice covers the document, not the ledger data.
- **Not readable**: "Distributed Compliance Ledger (DCL) Policies,
  Procedure and Governance" (groups.csa-iot.org/wg/members-all/document/26075)
  is members-only. It may carry conditions this read cannot see.

## Precedent

- connectedhomeip (Apache-2.0) commits the production PAA roots and the
  CD signing certificates to its repository
  (`credentials/production/paa-root-certs/`, `credentials/production/cd-certs/`),
  fetched by `credentials/fetch_paa_certs_from_dcl.py` (Apache-2.0 header,
  "fetch … Product Attestation Authority (PAA) certificates from DCL").
- matter.js at `0ceb202d` ships the mechanism only (`SeedSource` /
  `CertSeedEntry` in `packages/protocol/src/dcl/SeedTypes.ts`, consumed by
  `DclCertificateService.ts:336-340`, `#consumeCertSeed` :1352-1410), no
  data.

## Reading

The certificates are public by the PKI's own policy (the CP, which is
itself CC BY 4.0, requires their publication); the ledger is readable
without an account; the CSA's website terms do not reach ledger data; the
CSA's own reference implementation redistributes them under Apache-2.0.
No published document restricts redistribution. The residual risk is the
members-only governance document.

Obligations a snapshot must keep, from the CP: the relying party stays
responsible for status checks (§1.4.5, §4.9.6) — a snapshot never replaces
the CRL / revocation path, which stays online (ADR 0020 stage 6a); and the
snapshot must say where and when it was taken (the `SOURCE` file) so a
stale PAA list is visible.
