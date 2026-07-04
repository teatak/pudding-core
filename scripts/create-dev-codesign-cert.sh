#!/usr/bin/env bash
# Create the local-only code signing identity used by `make desktop-dev`.
set -euo pipefail
cd "$(dirname "$0")/.."

IDENTITY="${PUDDING_DEV_CODESIGN_IDENTITY:-${PUDDING_CODESIGN_IDENTITY:-Pudding Dev Local}}"
DEFAULT_KEYCHAIN="$(security default-keychain | sed 's/^[[:space:]]*//; s/^"//; s/"$//')"
KEYCHAIN="${PUDDING_DEV_KEYCHAIN:-$DEFAULT_KEYCHAIN}"
P12_PASSWORD="${PUDDING_DEV_CERT_PASSWORD:-pudding-dev-local}"

have_identity() {
  security find-identity -v -p codesigning "$KEYCHAIN" 2>/dev/null | grep -F "\"$IDENTITY\"" >/dev/null
}

if have_identity; then
  echo ">> code signing identity already exists: $IDENTITY"
  exit 0
fi

if ! command -v openssl >/dev/null 2>&1; then
  echo "error: openssl is required to create $IDENTITY" >&2
  exit 1
fi

tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/pudding-dev-cert.XXXXXX")"
trap 'rm -rf "$tmpdir"' EXIT

cat > "$tmpdir/openssl.cnf" <<EOF
[ req ]
prompt = no
distinguished_name = dn
x509_extensions = v3_codesign

[ dn ]
CN = $IDENTITY

[ v3_codesign ]
basicConstraints = critical, CA:true
keyUsage = critical, digitalSignature, keyCertSign, cRLSign
extendedKeyUsage = codeSigning
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid,issuer
EOF

echo ">> creating local code signing identity: $IDENTITY"
openssl req \
  -newkey rsa:2048 \
  -nodes \
  -keyout "$tmpdir/key.pem" \
  -x509 \
  -days 3650 \
  -out "$tmpdir/cert.pem" \
  -config "$tmpdir/openssl.cnf" \
  -sha256 >/dev/null 2>&1

openssl pkcs12 \
  -export \
  -keypbe PBE-SHA1-3DES \
  -certpbe PBE-SHA1-3DES \
  -macalg sha1 \
  -inkey "$tmpdir/key.pem" \
  -in "$tmpdir/cert.pem" \
  -out "$tmpdir/identity.p12" \
  -name "$IDENTITY" \
  -passout "pass:$P12_PASSWORD" >/dev/null 2>&1

echo ">> importing identity into $KEYCHAIN"
security import "$tmpdir/identity.p12" \
  -k "$KEYCHAIN" \
  -P "$P12_PASSWORD" \
  -T /usr/bin/codesign \
  -T /usr/bin/security >/dev/null

echo ">> trusting identity for code signing"
security add-trusted-cert \
  -r trustRoot \
  -p codeSign \
  -k "$KEYCHAIN" \
  "$tmpdir/cert.pem" >/dev/null

if ! have_identity; then
  echo "error: identity was imported but is not visible to codesign yet" >&2
  echo "       Open Keychain Access and set $IDENTITY trust for Code Signing to Always Trust." >&2
  exit 1
fi

echo ">> done. You can now run: make desktop-dev"
