import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["src/**/*.test.ts", "src/**/*.test.tsx", "../test/frontend/**/*.test.ts", "../test/frontend/**/*.test.tsx"],
  },
});
