// @ts-check
import { tanstackConfig } from '@tanstack/eslint-config'

/**
 * Shared ESLint flat config for every TypeScript package in the repo.
 * Formatting is Prettier's job, so no stylistic rules here.
 *
 * @type {import('eslint').Linter.Config[]}
 */
const config = [
  ...tanstackConfig,
  {
    rules: {
      'import/no-cycle': 'off',
      'import/order': 'off',
      'sort-imports': 'off',
      '@typescript-eslint/array-type': 'off',
      '@typescript-eslint/require-await': 'off',
      'pnpm/json-enforce-catalog': 'off',
    },
  },
  {
    ignores: [
      '**/*.gen.ts',
      '**/.output/**',
      '**/.tanstack/**',
      'eslint.config.js',
    ],
  },
]

export default config
