# Mobile account onboarding

O Tunnel supports Google sign-in and email one-time codes through the same OIDC
issuer. The browser-based sign-in screen belongs to the identity provider.
OVPN stores the assignment between the verified account and VPN access. It
does not send mail or store email OTPs, mailbox addresses, or OAuth secrets.

## The two codes

1. The user opens O Tunnel and signs in with Google or enters their email.
   The provider sends a short-lived one-time code to that mailbox. The user
   enters it in the browser. For Google, the provider supplies the verified
   address from that sign-in.
2. The operator creates the VPN user in Desk or with `ovpn user add`. The
   user record still controls quota, expiry, and access on the servers.
   In **Create setup code**, enter the recipient's email, or run:

   ```sh
   ovpn user invite --username alice --base-url https://delivery.example.net \
     --recipient-email alice@example.net
   ```

3. The operator sends that OVPN setup code or QR separately. The signed-in user
   pastes or scans it in O Tunnel. Only the matching verified mailbox can claim
   the code. On another phone the same account can sign in and update profiles
   without using the setup code again.

Example verification email for the identity provider:

> Subject: Your O Tunnel sign-in code
>
> Use the one-time code shown below on the O Tunnel sign-in page. The code
> expires in a few minutes. If you did not request it, ignore this message.

Use the provider's built-in OTP variable in the template. OVPN setup codes
belong in the operator's access-delivery channel, separate from sign-in mail.

## Identity and storage

The service verifies the issuer, native client audience, token signature,
expiry, and verified email. It lowercases and trims the email address. Plus
addresses and dots are preserved. The registry associates one HMAC of that
address with one issuer-scoped subject and its invitation. Signed-in users
without an assignment do not create registry rows.

A second provider subject with the same mailbox or a changed address for an
existing subject returns a conflict. The user can sign in through the original
method. To offer both methods for the same existing account, use the identity
provider's linking flow and authenticate both identities while keeping the
original subject as primary. Matching email alone grants no existing access.

Keep the account registry and email HMAC key with the private writable
invitation directory. Back them up together with the bundles and invitation
receipts. Use one account delivery origin and one registry for the mobile
fleet, even when the VPN servers use HA. Use consistent backups and a single
active writer if moving the delivery service. An existing subject-only
assignment acquires its email binding on the next verified sign-in.

## Provider and abuse controls

The reference setup uses Auth0 Universal Login with Passwordless Email OTP and
a Google social connection enabled for both native clients. Register the two
app redirect URIs with the same issuer and use public native clients with
Authorization Code and PKCE. Configure a production sender with SPF, DKIM,
and DMARC. Keep the SMTP credentials and the Google provider secret in the
identity provider. Only the public issuer, per-platform client ID, and public
delivery origin go into the mobile artifact.

- Enable provider throttling for email sends, resends, and verification
  attempts. Set a short expiry and require the newest single-use code.
  Enable a risk-based challenge where the selected connection supports it.
  Test resend flooding and repeated wrong codes before opening signup.
- Apply edge rate limits to sign-in and activation traffic as appropriate.
  OVPN limits activation to eight attempts per verified account per five
  minutes in the delivery process. A verified mailbox proves mailbox
  control. Operator-issued invitations decide who gets VPN access.
- Track sent, verified, blocked, and bounced events in the provider and mail
  service. OVPN logs only account activation outcomes. Disable request body,
  authorization header, email address, and secret URL logging at the proxy.
- In staging, test Google and email OTP, incorrect and expired codes, limits,
  second-device recovery, an expired token followed by sign-in with the same
  account, and a duplicate mailbox returning an account conflict.

[Auth0 email one-time codes](https://developer.auth0.com/resources/labs/authentication/passwordless-with-email)
· [Auth0 account linking](https://auth0.com/docs/manage-users/user-accounts/user-account-linking/link-user-accounts)

## Deployment modes

Use `--account-require-invite-email` on the account delivery origin for new
account-enabled distributions. Issue new email-bound codes before enabling
strict mode. Existing claimed accounts can refresh. Keep legacy anonymous
receipts on the original origin during the [cutover](subscriptions.md#account-based-delivery).

The main mobile distribution uses managed subscription codes and HTTPS links.
Account mode remains an opt-in deployment for installations that require a
verified identity.
The default unsigned iOS simulator build is for UI and build validation.
Publish the matching stable OVPN core first, pin that release in each client,
and then build and verify the independent artifacts.
