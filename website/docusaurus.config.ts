import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

/**
 * The Sentinel release this documentation set describes (FR-039, SC-011).
 * The site documents one release only; there is no version selector.
 */
const SENTINEL_RELEASE = 'v1.4.0';

/**
 * Hosting target. The two supported targets serve the site from structurally
 * different paths, and Docusaurus bakes `baseUrl` into every generated link and
 * asset at build time — so this cannot be a constant.
 *
 *   ghpages (default) — https://denisakp.github.io/sentinel/
 *   custom            — https://sentinel.denisakp.me/docs/
 *
 * `ghpages` stays the default until DNS for sentinel.denisakp.me resolves.
 *
 * WARNING: do not add static/CNAME or set a custom domain in the repository's
 * Pages settings before that DNS record exists. Configuring a custom domain
 * makes GitHub redirect the github.io URL to it; with DNS unresolved the site
 * becomes unreachable at BOTH addresses.
 */
const DEPLOY_TARGET = process.env.DEPLOY_TARGET ?? 'ghpages';

const HOSTING = {
  ghpages: {url: 'https://denisakp.github.io', baseUrl: '/sentinel/'},
  custom: {url: 'https://sentinel.denisakp.me', baseUrl: '/docs/'},
} as const;

const hosting = HOSTING[DEPLOY_TARGET as keyof typeof HOSTING];

if (!hosting) {
  throw new Error(
    `Unknown DEPLOY_TARGET "${DEPLOY_TARGET}". Expected one of: ${Object.keys(
      HOSTING,
    ).join(', ')}.`,
  );
}

const config: Config = {
  title: 'Sentinel',
  tagline: 'Automated database backup, restore, and disaster recovery',
  favicon: 'img/favicon.ico',

  future: {
    v4: true,
  },

  url: hosting.url,
  baseUrl: hosting.baseUrl,
  trailingSlash: false,

  organizationName: 'denisakp',
  projectName: 'sentinel',

  // Publishing must fail rather than ship a broken site (FR-027, SC-007).
  // `onBrokenAnchors` defaults to 'warn'; on a heavily cross-linked site a stale
  // heading anchor is the most probable form of rot, so it is raised to 'throw'.
  onBrokenLinks: 'throw',
  onBrokenAnchors: 'throw',

  markdown: {
    // `.md` is parsed as plain CommonMark, `.mdx` as MDX. Authors writing prose
    // should not have to know MDX's parsing quirks — notably that a bare `<!-- -->`
    // comment or a stray `<` is a syntax error. A page that genuinely needs React
    // components (tabs, for instance) opts in by using the .mdx extension.
    format: 'detect',
    hooks: {
      onBrokenMarkdownLinks: 'throw',
    },
  },

  // English only for now (FR-038). Declaring the default locale explicitly means
  // a translation added later gets a /<locale>/ prefix while every existing
  // English URL stays exactly where it is.
  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          // The site is documentation only — docs are served at the site root
          // rather than under an extra /docs segment, because the baseUrl
          // already carries the path.
          routeBasePath: '/',
          sidebarPath: './sidebars.ts',
          editUrl: 'https://github.com/denisakp/sentinel/tree/develop/website/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
        sitemap: {
          // pa11y-ci walks this sitemap to audit every published page.
          lastmod: 'date',
          changefreq: null,
          priority: null,
        },
      } satisfies Preset.Options,
    ],
  ],

  plugins: [
    [
      '@docusaurus/plugin-client-redirects',
      {
        // Published URLs are permanent (FR-044). Every entry here must have a
        // matching row in website/redirects.md explaining why the page moved
        // (FR-045). Redirect stubs are emitted by `npm run build` only — never
        // by the dev server.
        redirects: [],
      },
    ],
  ],

  themeConfig: {
    colorMode: {
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'Sentinel',
      logo: {
        alt: 'Sentinel logo',
        src: 'img/logo.svg',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docs',
          position: 'left',
          label: 'Documentation',
        },
        {
          href: 'https://github.com/denisakp/sentinel',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Documentation',
          items: [
            {label: 'What Sentinel is', to: '/'},
            {label: 'Installation', to: '/intro/installation'},
            {label: 'Quickstart', to: '/intro/quickstart'},
          ],
        },
        {
          title: 'Project',
          items: [
            {label: 'GitHub', href: 'https://github.com/denisakp/sentinel'},
            {
              label: 'Releases',
              href: 'https://github.com/denisakp/sentinel/releases',
            },
            {
              label: 'Report an issue',
              href: 'https://github.com/denisakp/sentinel/issues',
            },
          ],
        },
      ],
      // States which release this documentation describes (FR-039, SC-011).
      copyright: `Documentation for Sentinel ${SENTINEL_RELEASE}. Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['bash', 'yaml', 'json', 'go', 'sql'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
