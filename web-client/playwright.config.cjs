const { defineConfig } = require("@playwright/test");

module.exports = defineConfig({
  testDir: "./e2e",
  timeout: 30_000,
  use: {
    headless: true,
    trace: "retain-on-failure",
  },
  reporter: [["list"]],
});
