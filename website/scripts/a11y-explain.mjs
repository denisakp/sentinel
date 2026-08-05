/**
 * Explain an accessibility violation that the pa11y reporter names but does not
 * quantify.
 *
 * pa11y tells you *which* element failed. For a contrast failure that is rarely
 * enough — you need the foreground colour, the background colour, and the ratio
 * axe actually computed, which pa11y does not surface. This runs axe-core
 * directly and prints the full check data for every violation.
 *
 * Usage (with `npm run serve` already running):
 *   node scripts/a11y-explain.mjs http://localhost:3000/sentinel/intro/architecture-overview
 */
import puppeteer from 'puppeteer';
import {createRequire} from 'node:module';

const require = createRequire(import.meta.url);
const axePath = require.resolve('axe-core');
const axeSource = require('node:fs').readFileSync(axePath, 'utf8');

const url = process.argv[2];
if (!url) {
  console.error('usage: node scripts/a11y-explain.mjs <url>');
  process.exit(2);
}

const browser = await puppeteer.launch({
  args: ['--no-sandbox', '--disable-dev-shm-usage', '--disable-gpu'],
});

try {
  const page = await browser.newPage();
  await page.goto(url, {waitUntil: 'networkidle0'});
  await page.evaluate(axeSource);

  const results = await page.evaluate(async () =>
    // eslint-disable-next-line no-undef
    await axe.run(document, {runOnly: ['color-contrast', 'link-in-text-block']}),
  );

  for (const kind of ['violations', 'incomplete']) {
    const items = results[kind] ?? [];
    if (!items.length) continue;
    console.log(`\n===== ${kind.toUpperCase()} (${items.length}) =====`);
    for (const v of items) {
      console.log(`\n[${v.id}] ${v.help}`);
      for (const node of v.nodes) {
        console.log(`  target: ${node.target.join(' ')}`);
        console.log(`  html:   ${node.html.slice(0, 160)}`);
        for (const check of [...node.any, ...node.all, ...node.none]) {
          console.log(`  check:  ${check.id} — ${check.message}`);
          if (check.data) console.log(`  data:   ${JSON.stringify(check.data)}`);
        }
      }
    }
  }

  if (!(results.violations ?? []).length && !(results.incomplete ?? []).length) {
    console.log('no violations or incomplete results for the audited rules');
  }
} finally {
  await browser.close();
}
