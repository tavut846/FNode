# Implementation Plan - Hysteria2-FNode Server-Side Protocol Implementation

This plan details the server-side customizations for the new `hysteria2-fnode` option. These changes run exclusively on the server to make its fingerprint and network behavior differ from standard Hysteria2 servers, while remaining 100% compatible with unmodified, standard Hysteria2 clients (e.g. Clash Meta, sing-box, v2rayN).

## User Review Required

> [!IMPORTANT]
> - **How it differs from standard Hysteria2 (Server-Side Only)**:
>   1. **Silent UDP/QUIC Dropping**: Standard Hysteria2 servers respond to unrecognized QUIC handshakes or invalid obfs packets (e.g. with version negotiation or connection close). `hysteria2-fnode` will silently drop/ignore all UDP packets that fail initial obfuscation or authentication checks, making the port appear completely closed to GFW active probes.
>   2. **Randomized TLS ServerHello Fingerprint**: Customize TLS handshake behavior (e.g., dynamically altering cipher suite preference order, handshake message padding, and extension parameters) on the server side. Unmodified clients will still connect successfully, but GFW scanners looking for standard sing-box Hysteria2 TLS signatures will fail to match them.
>   3. **Advanced HTTP/3 Masquerading**: When probed via standard HTTP/3, the server responds with customized HTTP headers mimicking mainstream web servers (like Nginx/IIS) and redirects or reverse-proxies to a configured legitimate site.
>
> - **No Client Changes**: The client still uses standard Hysteria2 protocol configurations.

## Proposed Changes

### 1. Panel Integration & Type Mapping

#### [MODIFY] [panel.go](file:///C:/Users/Keanghour/Documents/GitHub/FNode/FNode/api/panel/panel.go)
- Accept `"hysteria2-fnode"` in FNode configuration.
- Request `"hysteria2"` from the panel API to ensure compatibility with Xboard.

#### [MODIFY] [node.go](file:///C:/Users/Keanghour/Documents/GitHub/FNode/FNode/api/panel/node.go)
- Decode panel node params for both `"hysteria2"` and `"hysteria2-fnode"` into `Hysteria2Node`.

### 2. Sing-box Wrapper Customizations

#### [MODIFY] [node.go](file:///C:/Users/Keanghour/Documents/GitHub/FNode/FNode/core/sing/node.go)
- Identify if the inbound node type is `hysteria2-fnode`.
- Inject specific custom options to the sing-box Hysteria2 inbound:
  - Configure default masquerade upstream (e.g., a highly reputable local news or static site).
  - Customize the TLS configuration: disable standardized TLS fingerprints, randomize curve preferences, and adjust TLS/QUIC handshake parameters.
  - Implement a custom UDP socket listener/filter layer if needed, or configure sing-box's underlying QUIC server to silently ignore handshake failures instead of replying.

## Verification Plan

### Automated Tests
- Validate and compile:
  ```bash
  go build -v -o FNode.exe
  ```

### Manual Verification
- Run FNode as `hysteria2-fnode`.
- Verify standard Hysteria2 clients can connect and transfer data smoothly.
- Test port scanning on the UDP port and verify that invalid packets receive no response (silent drop).
