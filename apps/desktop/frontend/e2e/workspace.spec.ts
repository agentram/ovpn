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
  await dialog
    .getByRole("button", { name: "More actions", exact: true })
    .click();
  await dialog
    .getByRole("searchbox", { name: "Find an action" })
    .fill("server.profile.list");
  await dialog
    .locator(".desk-action-list")
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
    .getByRole("searchbox", { name: "Find an action" })
    .fill("user.probe");
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

test("user actions stay compact and searchable without unrelated controls", async ({
  page,
}) => {
  await page.goto("/");
  await page
    .getByRole("navigation")
    .getByRole("button", { name: "Users", exact: true })
    .click();
  await page.getByRole("searchbox").fill("bob");
  await page.getByRole("button", { name: "bob", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.locator(".desk-detail-actions > button")).toHaveCount(3);
  await expect(
    dialog.getByRole("button", { name: "Change traffic limit", exact: true }),
  ).toHaveCount(0);
  await dialog
    .getByRole("button", { name: "More actions", exact: true })
    .click();
  await dialog
    .getByRole("searchbox", { name: "Find an action" })
    .fill("user.quota-set");
  await expect(dialog.locator(".desk-action-list > button")).toHaveCount(1);
  await dialog
    .getByRole("button", { name: "Change traffic limit", exact: true })
    .click();
  await expect(
    page
      .getByRole("dialog")
      .getByRole("heading", { name: "Change traffic limit", exact: true }),
  ).toBeVisible();
});

test("entry-server setup requires an eligible backend and explains its failure boundary", async ({
  page,
}) => {
  await page.goto("/");
  await page
    .getByRole("navigation")
    .getByRole("button", { name: "Servers", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Add server", exact: false })
    .first()
    .click();
  const dialog = page.getByRole("dialog");
  await dialog
    .getByRole("combobox", { name: "Connection type", exact: true })
    .selectOption("proxy");
  await expect(
    dialog.getByRole("button", { name: "Continue", exact: true }),
  ).toBeDisabled();
  await expect(dialog.getByRole("checkbox")).toHaveCount(1);
  await dialog
    .getByRole("checkbox", { name: "Backup fixture", exact: true })
    .check();
  await expect(
    dialog.getByRole("button", { name: "Continue", exact: true }),
  ).toBeEnabled();
  await expect(dialog.getByText(/single point of failure/)).toBeVisible();
});

test("HA topology reviews backend-first application without writing to any server", async ({
  page,
  request,
}) => {
  expect((await request.post("/fixture/add-ha")).status()).toBe(204);
  await page.goto("/");
  await page
    .getByRole("navigation")
    .getByRole("button", { name: "Servers", exact: true })
    .click();
  await page
    .getByRole("button")
    .filter({
      has: page.getByRole("heading", { name: "Entry fixture", exact: true }),
    })
    .click();
  let dialog = page.getByRole("dialog");
  await expect(
    dialog.getByRole("region", { name: "High availability" }),
  ).toBeVisible();
  await expect(dialog.locator(".desk-ha-backend")).toHaveCount(2);
  await expect(dialog.getByText(/single point of failure/)).toBeVisible();
  await expect(
    dialog.getByText("Backend status is not available", { exact: true }),
  ).toBeVisible();
  if (process.env.OVPN_DESK_SCREENSHOTS)
    await page.screenshot({
      path: `${process.env.OVPN_DESK_SCREENSHOTS}/ha-topology.png`,
      fullPage: true,
    });
  await dialog
    .getByRole("button", { name: "Apply HA configuration", exact: true })
    .click();
  dialog = page.getByRole("dialog");
  await dialog
    .getByRole("button", { name: "Review changes", exact: true })
    .click();
  await expect(dialog.locator(".desk-targets li")).toHaveText([
    "Primary fixture",
    "Backup fixture",
    "Entry fixture",
  ]);
  await expect(dialog.getByText(/A failed step stops/)).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: "Apply changes", exact: true }),
  ).toBeEnabled();
});

test("translated action menus wrap long labels and remain keyboard accessible", async ({
  page,
}) => {
  const translations = [
    {
      lang: "en",
      users: "Users",
      more: "More actions",
      search: "Find an action",
    },
    {
      lang: "ru",
      users: "Пользователи",
      more: "Ещё",
      search: "Найти действие",
    },
    { lang: "zh", users: "用户", more: "更多操作", search: "搜索操作" },
  ];
  await page.goto("/");
  for (const copy of translations) {
    await page.locator(".desk-sidebar button").last().click();
    await page
      .getByRole("dialog")
      .getByRole("combobox")
      .first()
      .selectOption(copy.lang);
    await expect(page.locator("html")).toHaveAttribute("lang", copy.lang);
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await page.setViewportSize({ width: 960, height: 820 });
    await page.addStyleTag({
      content:
        ".desk-action-list button,.desk-action-categories button{font-size:20px}",
    });
    await page
      .getByRole("navigation")
      .getByRole("button", { name: copy.users, exact: true })
      .click();
    await page.getByRole("searchbox").fill("bob");
    await page.getByRole("button", { name: "bob", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("button", { name: copy.more, exact: true }).focus();
    await page.keyboard.press("Enter");
    await expect(
      dialog.getByRole("searchbox", { name: copy.search }),
    ).toBeVisible();
    await expect(dialog.locator(".desk-action-list > button")).not.toHaveCount(
      0,
    );
    expect(
      await dialog
        .locator(".desk-action-list > button")
        .evaluateAll((buttons) =>
          buttons.every((button) => button.scrollWidth <= button.clientWidth),
        ),
    ).toBe(true);
    if (copy.lang === "ru" && process.env.OVPN_DESK_SCREENSHOTS)
      await page.screenshot({
        path: `${process.env.OVPN_DESK_SCREENSHOTS}/user-menu-ru.png`,
        fullPage: true,
      });
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toHaveCount(0);
  }
});
