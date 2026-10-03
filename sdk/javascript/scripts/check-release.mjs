import fs from 'node:fs';

const pkg = JSON.parse(fs.readFileSync('package.json', 'utf8'));
if (pkg.name !== '@somprasongd/thai-smartcard-client') throw new Error('Unexpected package name');
if (pkg.license !== 'Apache-2.0' || !fs.existsSync('LICENSE') || !fs.existsSync('NOTICE')) {
  throw new Error('The SDK must include Apache-2.0 LICENSE and NOTICE before publishing');
}
if (!/^\d+\.\d+\.\d+$/.test(pkg.version)) throw new Error('This workflow publishes stable SDK versions only');
const changelog = fs.readFileSync('CHANGELOG.md', 'utf8');
if (!changelog.includes(`## [${pkg.version}] - `)) throw new Error('Version the SDK changelog before publishing');
if (process.env.GITHUB_ACTIONS === 'true') {
  if (process.env.GITHUB_REF_NAME !== `sdk-v${pkg.version}`) throw new Error('SDK tag must match package version');
  if (process.env.GITHUB_REPOSITORY !== 'somprasongd/go-thai-smartcard') throw new Error('Unexpected publisher repository');
}
