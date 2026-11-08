import config from '@esa/eslint-config'

export default [
  ...config,
  {
    ignores: [
      'dist/**',
      '.astro/**',
      'src/content/**',
      'src/**/*.astro',
      'public/**',
    ],
  },
]
