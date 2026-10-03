# Web client examples

From the repository root, build the SDK and serve this checkout:

```sh
cd sdk/javascript
npm ci
npm run build
cd ../..
python3 -m http.server 8080 --bind 127.0.0.1
```

Open `http://127.0.0.1:8080/examples/web-client/`. Start the native agent at
`ws://127.0.0.1:9898/ws` and allow the example's origin in your agent settings.
If your agent requires a socket token, supply it at runtime using the SDK's
`token` option. Do not commit a real token into this example.

The example uses the local ESM build, displays connection/reader status, clears
card data on disconnection, and catches command errors. Browser local-network
permissions may also be needed. Use synthetic data when developing; displaying
a real card requires a physical reader/card and the native agent.

`consumer.ts` shows the npm import for a TypeScript application. After the SDK
is published, install `@somprasongd/thai-smartcard-client` in that application.
