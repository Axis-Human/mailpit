import { expect, test } from "@playwright/test";

// These tests run against a Mailpit started with --multi-project, see playwright.config.js.
const ADMIN = { email: "admin@example.com", password: "admin-password" };

// unique names, so the tests can be repeated against the same instance
const run = Date.now().toString(36);
const acme = { name: `Acme ${run}`, slug: `acme-${run}` };
const other = { name: `Other ${run}`, slug: `other-${run}` };
const dev = { email: `dev-${run}@example.com`, password: "dev-password-1" };

// the sidebar is rendered twice (desktop & mobile menu), only one is visible
const visible = (page, testId) => page.locator(`[data-testid="${testId}"]:visible`);

// the message in the list, found by its subject
const subject = (page, text) => page.getByTestId("message-row").filter({ hasText: text });

const login = async (page, { email, password }) => {
	await page.goto("");
	await page.getByLabel("Email").fill(email);
	await page.getByLabel("Password").fill(password);
	await page.getByRole("button", { name: "Sign in" }).click();
};

const createProject = async (page, { name, slug }) => {
	const form = page.getByTestId("project-form");
	await form.getByLabel("Project name").fill(name);
	await form.getByLabel("Slug").fill(slug);
	await form.getByRole("button", { name: "Create project" }).click();
	const key = page.getByTestId("api-key");
	await expect(key).toBeVisible();
	const value = await key.inputValue();
	expect(value).toMatch(/^mpk_/);
	// Bootstrap ignores a close request while the dialog is still fading in, so retry
	await expect(async () => {
		await page.getByRole("button", { name: "Done" }).click();
		await expect(page.locator("#ApiKeyModal")).toBeHidden({ timeout: 1000 });
	}).toPass();
	return value;
};

const send = (request, apiKey, subject) =>
	request.post("api/v1/send", {
		headers: { "X-API-Key": apiKey },
		data: {
			From: { Email: "sender@example.com" },
			To: [{ Email: "to@example.com" }],
			Subject: subject,
			Text: "Body",
		},
	});

test.describe.configure({ mode: "serial" });

const keys = {};

test("requires a login, and shows an error for a wrong password", async ({ page, request }) => {
	await page.goto("");
	await expect(page.getByTestId("login-form")).toBeVisible();

	// nothing is available without logging in
	expect((await request.get("api/v1/messages")).status()).toBe(401);

	await page.getByLabel("Email").fill(ADMIN.email);
	await page.getByLabel("Password").fill("wrong-password");
	await page.getByRole("button", { name: "Sign in" }).click();
	await expect(page.getByTestId("login-error")).toHaveText("invalid email or password");
});

test("a super admin creates projects and receives messages per project", async ({ page, request }) => {
	await login(page, ADMIN);
	await page.getByRole("link", { name: "Administration" }).click();
	await expect(page.getByTestId("projects-table")).toBeVisible();

	keys.acme = await createProject(page, acme);
	keys.other = await createProject(page, other);
	await expect(page.getByTestId(`project-${acme.slug}`)).toBeVisible();

	expect((await send(request, keys.acme, `Hello Acme ${run}`)).status()).toBe(200);
	expect((await send(request, keys.other, `Hello Other ${run}`)).status()).toBe(200);

	// the inbox shows the messages of the selected project only
	await page.getByRole("link", { name: "Back to inbox" }).click();
	await visible(page, "project-select").selectOption(acme.slug);
	await expect(subject(page, `Hello Acme ${run}`)).toBeVisible();
	await expect(subject(page, `Hello Other ${run}`)).toHaveCount(0);

	await visible(page, "project-select").selectOption(other.slug);
	await expect(subject(page, `Hello Other ${run}`)).toBeVisible();
	await expect(subject(page, `Hello Acme ${run}`)).toHaveCount(0);
});

test("new messages appear live in the selected project only", async ({ page, request }) => {
	await login(page, ADMIN);
	await visible(page, "project-select").selectOption(acme.slug);
	await expect(subject(page, `Hello Acme ${run}`)).toBeVisible();

	expect((await send(request, keys.other, `Live Other ${run}`)).status()).toBe(200);
	expect((await send(request, keys.acme, `Live Acme ${run}`)).status()).toBe(200);

	await expect(subject(page, `Live Acme ${run}`)).toBeVisible();
	await expect(subject(page, `Live Other ${run}`)).toHaveCount(0);
});

test("a user only sees the projects assigned to them", async ({ page }) => {
	await login(page, ADMIN);
	await visible(page, "admin-link").click();
	await page.getByTestId("tab-users").click();
	await page.getByTestId("add-user").click();

	const form = page.getByTestId("user-form");
	await form.getByLabel("Email").fill(dev.email);
	await form.getByLabel("Password").fill(dev.password);
	await form.getByLabel(acme.name).check();
	await form.getByRole("button", { name: "Save" }).click();
	await expect(page.getByTestId(`user-${dev.email}`)).toBeVisible();

	await page.goto("");
	await visible(page, "logout").click();
	await expect(page.getByTestId("login-form")).toBeVisible();

	await login(page, dev);
	const select = visible(page, "project-select");
	await expect(select.locator("option")).toHaveCount(1);
	await expect(select.locator("option")).toHaveText(acme.name);
	await expect(subject(page, `Hello Acme ${run}`)).toBeVisible();
	await expect(visible(page, "admin-link")).toHaveCount(0);

	// the administration is not available
	await page.goto("admin");
	await expect(page).not.toHaveURL(/admin/);
});

test("disabling a project rejects its API key", async ({ page, request }) => {
	await login(page, ADMIN);
	await visible(page, "admin-link").click();

	const row = page.getByTestId(`project-${acme.slug}`);
	await row.getByTestId("toggle-project").click();
	await expect(row.getByText("Disabled", { exact: true }).first()).toBeVisible();

	expect((await send(request, keys.acme, "while disabled")).status()).toBe(403);
	expect((await request.get("api/v1/messages", { headers: { "X-API-Key": keys.acme } })).status()).toBe(403);
	// other projects are not affected
	expect((await send(request, keys.other, `Still works ${run}`)).status()).toBe(200);

	await row.getByTestId("toggle-project").click();
	await expect(row.getByText("Enabled", { exact: true })).toBeVisible();
	expect((await send(request, keys.acme, `Enabled again ${run}`)).status()).toBe(200);
});
