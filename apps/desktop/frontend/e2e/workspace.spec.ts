import { expect, test } from "@playwright/test";

test("users can be found across inventory pages and access is disclosed only on request", async ({
  page,
}) => {
  await page.goto("/");
  await page
    .getByRole("navigation")
    .getByRole("button", { name: "Users", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "expired-51", exact: true }),
  ).toBeVisible();
  await expect(page.locator("body")).not.toContainText("vless://");
  await page.getByRole("searchbox").fill("bob");
  await page.getByRole("button", { name: "bob", exact: true }).click();
  let dialog = page.getByRole("dialog");
  await dialog
    .getByRole("button", { name: "Share access", exact: true })
    .click();
  dialog = page.getByRole("dialog");
  await expect(
    dialog.getByRole("img", { name: "Scan to connect" }),
  ).toBeVisible({ timeout: 20000 });
  await expect(
    dialog.getByRole("textbox", { name: "Share access" }),
  ).toHaveValue(/^vless:\/\//);
  await dialog
    .getByRole("button", { name: "Close", exact: true })
    .last()
    .click();
  await expect(page.locator("body")).not.toContainText("vless://");
});

test("creating access reviews every enabled server before any write", async ({
  page,
}) => {
  await page.goto("/");
  await page
    .getByRole("button", { name: "Add user", exact: false })
    .first()
    .click();
  const dialog = page.getByRole("dialog");
  await dialog
    .getByRole("textbox", { name: "User", exact: true })
    .fill("new-user");
  await dialog.getByRole("button", { name: "Review changes" }).click();
  await expect(
    dialog.getByText("Primary fixture", { exact: true }),
  ).toBeVisible();
  await expect(
    dialog.getByText("Backup fixture", { exact: true }),
  ).toBeVisible();
  await dialog.getByRole("button", { name: "Back", exact: true }).click();
  await expect(
    dialog.getByRole("textbox", { name: "User", exact: true }),
  ).toHaveValue("new-user");
  await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
  await page
    .getByRole("navigation")
    .getByRole("button", { name: "Users", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "new-user", exact: true }),
  ).toHaveCount(0);
});

test("server details execute the existing profile action with its positional server argument", async ({
  page,
}) => {
  await page.goto("/");
  await page
    .getByRole("navigation")
    .getByRole("button", { name: "Servers", exact: true })
    .click();
  await page
    .getByRole("button")
    .filter({
      has: page.getByRole("heading", { name: "Primary fixture", exact: true }),
    })
    .click();
  let dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "Advanced", exact: false }).click();
  await dialog
    .getByRole("button", { name: "Connection methods", exact: true })
    .click();
  dialog = page.getByRole("dialog");
  await expect(
    dialog.getByRole("heading", { name: "Completed", exact: true }),
  ).toBeVisible({ timeout: 15000 });
  await expect(dialog.locator("pre")).toContainText("tls");
});

test("three languages, light theme, small window and enlarged text remain usable", async ({
  page,
}) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Settings", exact: false }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Appearance").selectOption("light");
  await dialog.getByLabel("Language").selectOption("zh");
  await expect(page.locator("html")).toHaveAttribute("lang", "zh");
  await dialog.getByLabel("语言").selectOption("ru");
  await dialog.getByLabel("Язык").selectOption("en");
  await dialog
    .getByRole("button", { name: "Close", exact: true })
    .last()
    .click();
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await page.setViewportSize({ width: 960, height: 720 });
  await page.addStyleTag({ content: "html{font-size:24px}" });
  await page
    .getByRole("navigation")
    .getByRole("button", { name: "Users", exact: true })
    .click();
  await expect(page.getByRole("searchbox")).toBeVisible();
  await page.getByRole("searchbox").fill("alice");
  await page.getByRole("button", { name: "alice", exact: true }).focus();
  await page.keyboard.press("Enter");
  await expect(
    page
      .getByRole("dialog")
      .getByRole("heading", { name: "alice", exact: true }),
  ).toBeVisible();
});

test("a stale reviewed change fails before backup or remote writes", async ({
  page,
  request,
}) => {
  await page.goto("/");
  await page
    .getByRole("button", { name: "Add user", exact: false })
    .first()
    .click();
  const dialog = page.getByRole("dialog");
  await dialog
    .getByRole("textbox", { name: "User", exact: true })
    .fill("stale-user");
  await dialog.getByRole("button", { name: "Review changes" }).click();
  expect((await request.post("/fixture/disable-alice")).status()).toBe(204);
  await dialog.getByRole("button", { name: "Apply changes" }).click();
  await expect(
    dialog.getByText(
      "The configuration changed. Review a new plan before applying.",
    ),
  ).toBeVisible({ timeout: 15000 });
  await dialog.getByText("Details", { exact: true }).click();
  await expect(
    dialog.locator("code").filter({ hasText: "STALE_PLAN" }),
  ).toBeVisible();
  await dialog
    .getByRole("button", { name: "Close", exact: true })
    .last()
    .click();
  await page
    .getByRole("navigation")
    .getByRole("button", { name: "Activity", exact: true })
    .click();
  await expect(
    page
      .getByRole("row")
      .filter({ hasText: "Add user" })
      .getByText("Failed", { exact: true }),
  ).toBeVisible();
});

test("automatic checks discover eligible assignments without reporting probes as connected users", async ({
  page,
}) => {
  await page.goto("/");
  await page
    .getByRole("button", { name: "Enable automatic checks", exact: true })
    .click();
  await expect(
    page.getByText("Automatic checks are running", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("navigation")
    .getByRole("button", { name: "Users", exact: true })
    .click();
  await page.getByRole("searchbox").fill("bob");
  await page.getByRole("button", { name: "bob", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(
    dialog.getByRole("table").getByText("REALITY", { exact: true }),
  ).toBeVisible({ timeout: 15000 });
  await expect(
    dialog.getByRole("table").getByText("Connection failed", { exact: true }),
  ).toBeVisible({ timeout: 35000 });
  await expect(dialog.getByText("Connected now", { exact: true })).toHaveCount(
    0,
  );
  await dialog
    .getByRole("button", { name: "Close", exact: true })
    .last()
    .click();
  await page
    .getByRole("button", { name: "Pause automatic checks", exact: true })
    .click();
  await expect(
    page.getByText("Automatic checks are paused", { exact: true }).first(),
  ).toBeVisible();
});

test("linked design screens expose the main user journey", async ({ page }) => {
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Your OVPN workspace" }),
  ).toBeVisible();
  await expect(page.getByRole("status")).toHaveCount(0);
  const capture = async (name: string) => {
    if (process.env.OVPN_DESK_SCREENSHOTS)
      await page.screenshot({
        path: `${process.env.OVPN_DESK_SCREENSHOTS}/${name}.png`,
        fullPage: true,
      });
  };
  await capture("overview");
  await page
    .getByRole("navigation")
    .getByRole("button", { name: "Users", exact: true })
    .click();
  await page.getByRole("searchbox").fill("bob");
  await capture("users");
  await page
    .getByRole("button", { name: "Add user", exact: false })
    .first()
    .click();
  await capture("add-user");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Cancel", exact: true })
    .click();
  await page
    .getByRole("navigation")
    .getByRole("button", { name: "Servers", exact: true })
    .click();
  await page
    .getByRole("button")
    .filter({
      has: page.getByRole("heading", { name: "Primary fixture", exact: true }),
    })
    .click();
  await capture("server");
});

test("connection checks offer only eligible users assigned to the selected server", async ({
  page,
}) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Settings", exact: false }).click();
  await page
    .getByRole("dialog")
    .getByText("Advanced settings", { exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Check now", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog
    .getByRole("combobox", { name: "Server", exact: true })
    .selectOption({ label: "Backup fixture" });
  const users = dialog.getByRole("combobox", { name: "User", exact: true });
  await expect(users.locator('option:not([value=""])')).toHaveText(["bob"]);
  await users.selectOption("bob");
  await dialog
    .getByRole("combobox", { name: "Server", exact: true })
    .selectOption({ label: "Primary fixture" });
  await expect(users).toHaveValue("");
  await expect(users.locator('option:not([value=""])')).toHaveCount(0);
});
