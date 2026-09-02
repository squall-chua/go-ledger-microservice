import { defineVitestConfig } from '@nuxt/test-utils/config'

// The util tests are plain functions and run fastest in the default node
// environment, so that stays the default. A test that needs a Nuxt runtime —
// one exercising a composable — opts in with `// @vitest-environment nuxt` on
// its first line rather than making every file pay for the app context.
export default defineVitestConfig({})
