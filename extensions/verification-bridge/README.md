# Octopus Verification Bridge

Manifest V3 browser extension for completing paired Site verification tasks.

## Load

1. Open the browser extension management page.
2. Enable developer mode.
3. Load this directory as an unpacked extension.
4. Create an account-scoped pairing in Octopus and enter the returned pairing token in the extension popup. The token is shown once, remains valid until expiry/revocation, and can only claim tasks for that Site Account.

## Flow

1. Save the Octopus URL and pairing token.
2. Claim a pending verification task.
3. Open the dedicated verification window and complete the Site challenge.
4. Submit the target Site cookies to Octopus.

The extension requests host access only for the configured Octopus instance and the current verification target. Target access is removed after submission or explicit release. Pairing tokens remain in extension-local storage and are never sent anywhere except the configured Octopus instance.
