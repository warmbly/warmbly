import { expect, test } from "../lib/proof.ts";
import { env } from "../lib/env.ts";

// Runs against the store simulator (`GRAPHKIT_API_URL=fake`, the `make backend` default): any
// domain label longer than four characters is available, and an order reaches ready in seconds.
const REGISTRANT: Record<string, string> = {
  Phone: "+1.4155550100",
  "Street address": "1 Market St",
  City: "San Francisco",
  "State or region": "CA",
  "Postal code": "94105",
  "Country code": "US",
};

test("set three senders once, buy two domains with a mailbox per sender, and watch them connect", async ({ page, proof }) => {
  test.setTimeout(180_000);
  // Report the product tour as finished, so a freshly seeded account never covers the page with it.
  await page.route("**/v1/auth/me", async (route) => {
    const response = await route.fetch();
    await route.fulfill({ response, json: { ...await response.json(), product_tour_completed_at: "2026-01-01T00:00:00Z" } });
  });

  await page.goto("/app/emails");
  await page.getByRole("button", { name: "Add account" }).first().click();
  await expect(page.getByRole("menuitem", { name: /Buy new mailboxes/ })).toBeVisible();
  await proof.chapter("Entry", "Buying sits first in the Add account menu on the Mailboxes page");
  await proof.shot("entry-menu", { caption: "Buy new mailboxes, $3 a month, leads the Add account menu" });
  await page.getByRole("menuitem", { name: /Buy new mailboxes/ }).click();

  await expect(page.getByRole("heading", { name: "Buy mailboxes" })).toBeVisible();
  await expect(page.getByText("Search by your brand name")).toBeVisible();
  const summary = page.getByRole("region", { name: "Order summary" });
  await expect(summary).toBeVisible();
  await expect(summary.getByRole("list", { name: "Before you can pay" })).toBeVisible();
  await proof.chapter("One screen", "Search, senders and the order across the page; the summary and Pay in a sticky column");
  await proof.shot("search-empty", { caption: "The store opens on a search, with a checklist of what the order needs above Pay" });

  await page.getByPlaceholder("Your brand, like acme").pressSequentially("northwind", { delay: 70 });
  const exact = page.getByRole("button", { name: /^Add northwind\.com for/ });
  await expect(exact).toBeVisible({ timeout: 15_000 });
  await expect(page.getByText("Exact match").first()).toBeVisible();
  await proof.dwell();
  await proof.shot("search-results", { caption: "Exact match first, then suggestions, each with its first-year and renewal price" });

  await exact.click();
  await page.getByRole("button", { name: /^Add .+ for \$/ }).first().click();
  const order = page.getByRole("region", { name: "Your order" });
  await expect(order.getByRole("article")).toHaveCount(2);
  await expect(summary.getByText("Due today")).toBeVisible();
  await proof.dwell();
  await proof.shot("order-two", { caption: "Each added domain slides into the order with three mailboxes for you: dev@, dev.u@ and dev.user@" });

  // Two more senders, set once: every domain gets a mailbox for each of them.
  const senders = page.getByRole("region", { name: "Senders" });
  for (const [n, first, last] of [[2, "Grace", "Hopper"], [3, "Alan", "Turing"]] as const) {
    await senders.getByRole("button", { name: "Add sender" }).click();
    await senders.getByLabel(`Sender ${n} first name`).pressSequentially(first, { delay: 40 });
    await senders.getByLabel(`Sender ${n} last name`).pressSequentially(last, { delay: 40 });
  }
  const first = order.getByRole("article", { name: "northwind.com" });
  await expect(first.getByRole("list", { name: "Mailboxes on northwind.com" }).getByRole("listitem")).toHaveCount(3);
  await expect(first.getByText("grace.hopper@")).toBeVisible();
  await expect(senders.getByText(/^3 mailboxes per domain · .+ · Google · Signature: Default$/)).toBeVisible();
  await proof.chapter("Senders", "Names and photos, set once for every domain, with one line saying what each domain gets");
  await proof.dwell();
  await proof.shot("senders", { caption: "Three senders set once: every domain gets grace.hopper@, alan.turing@ and yours, with their photos" });

  // Customize holds the addresses, provider, count and signature; the defaults above need none of it.
  await senders.getByRole("button", { name: /^Customize$/ }).click();
  const custom = senders.getByRole("group", { name: "Customize the mailboxes" });
  await custom.getByRole("button", { name: /^Address patterns:/ }).click();
  const patterns = page.getByRole("dialog", { name: "Address patterns" });
  await expect(patterns.getByRole("checkbox", { name: /^First and last name with a dot/ })).toHaveAttribute("aria-checked", "true");
  await patterns.getByRole("checkbox", { name: /^First initial and last name with a dot/ }).click();
  await proof.chapter("Customize", "Address patterns, provider, mailboxes per sender and the signature, behind one button");
  await proof.dwell();
  await proof.shot("patterns", { caption: "Address patterns, each shown on the first sender's name, picked and ordered for every domain" });
  await page.keyboard.press("Escape");
  await expect(patterns).toHaveCount(0);

  // A custom signature is written in the mailbox signature editor, with the bulk signature's sender placeholders.
  await custom.getByRole("radiogroup", { name: "Signature" }).getByRole("radio", { name: "Custom" }).click();
  await expect(page.locator("#buy-signature")).toBeVisible();
  await custom.getByRole("button", { name: /Insert placeholder/ }).click();
  await page.getByRole("menu", { name: "Insert placeholder" }).getByRole("menuitem", { name: /^Email/ }).click();
  await expect(page.locator("#buy-signature")).toContainText("{{.Sender.Email}}");
  await expect(custom.getByLabel(/^Signature preview for .+@northwind\.com$/)).toBeVisible();
  await proof.chapter("Signature", "One template with sender placeholders, filled in per mailbox and set on each when it connects");
  await proof.dwell();
  await proof.shot("signature", { caption: "The signature editor with a sender placeholder inserted, previewed for the first mailbox" });

  await first.getByRole("radio", { name: "Microsoft 365" }).click();
  await first.getByRole("radio", { name: "Google Workspace" }).click();
  await first.getByRole("button", { name: /^Customize/ }).click();
  await expect(first.getByLabel("Address 1 on northwind.com")).toBeVisible();
  await proof.chapter("Your order", "Each card can switch provider or customize its own mailboxes");
  await proof.dwell();
  await proof.shot("order-configured", { caption: "Customize one domain's mailboxes, senders and admin without touching the others" });
  await first.getByRole("button", { name: /^Done/ }).click();

  // Registrant details are saved quietly; the summary only asks for what billing details left out.
  const registrant = summary.getByRole("group", { name: "Domain registrant" });
  if (await registrant.isVisible()) {
    for (const [label, value] of Object.entries(REGISTRANT)) {
      const field = registrant.getByLabel(label, { exact: true });
      if ((await field.count()) > 0 && (await field.inputValue()) === "") {
        await field.click();
        await field.pressSequentially(value, { delay: 30 });
      }
    }
  }
  await expect(summary.getByRole("button", { name: /^(Place order|Pay )/ })).not.toHaveAttribute("aria-disabled", "true", { timeout: 20_000 });
  await expect(summary.getByRole("list", { name: "Before you can pay" })).toHaveCount(0);
  await proof.chapter("Summary", "Itemised, priced live, one button");
  await proof.dwell();
  await proof.shot("summary", { caption: "Domains at cost, mailboxes at $3 a month, due today and what is charged after" });

  await summary.getByRole("button", { name: /^(Place order|Pay )/ }).click();
  // A session that signed in a moment ago is fresh enough; an older one confirms first.
  const reauth = page.getByRole("alertdialog", { name: "Confirm it is you" });
  const track = page.getByRole("list", { name: "Setup steps" });
  await expect(reauth.or(track)).toBeVisible({ timeout: 30_000 });
  if (await reauth.isVisible()) {
    await reauth.getByRole("textbox", { name: "Password" }).pressSequentially(env.password, { delay: 40 });
    await proof.shot("reauth", { caption: "Paying asks for a fresh sign-in, like every money change in Warmbly" });
    await reauth.getByRole("button", { name: "Confirm" }).click();
  }
  await expect(page.getByRole("list", { name: "Setup steps" })).toBeVisible({ timeout: 30_000 });
  await proof.chapter("Setting up", "Registration, DNS, mail authentication, mailboxes and connection, live");
  await proof.dwell();
  await expect(page.getByRole("progressbar", { name: "Setup progress" })).toBeVisible();
  await proof.shot("setup-running", { caption: "The setup track advances live: a step counter, the current activity and a rail that fills" });

  await expect(page.getByText(/mailboxes are ready/)).toBeVisible({ timeout: 120_000 });
  await expect(page.getByText("Needs attention")).toHaveCount(0);
  await proof.dwell();
  await proof.shot("setup-done", { caption: "Ready: the new mailboxes are connected and warming up" });

  await page.getByRole("link", { name: /Go to mailboxes/ }).or(page.getByRole("button", { name: /Go to mailboxes/ })).click();
  const bought = page.getByRole("row", { name: /^dev\.user@northwind\.com/ });
  await expect(bought).toBeVisible({ timeout: 30_000 });
  await expect(bought.getByText("Purchased")).toBeVisible();
  await proof.chapter("Owned", "Bought mailboxes sit with the rest, marked Purchased");
  await proof.shot("mailboxes-purchased", { caption: "The new mailboxes in the list with a Purchased chip" });

  await page.goto("/app/emails/domains");
  await page.getByRole("tab", { name: "Purchased" }).or(page.getByRole("button", { name: "Purchased" })).first().click();
  await expect(page.getByText("northwind.com").first()).toBeVisible();
  await proof.dwell();
  await proof.shot("purchased-domains", { caption: "Purchased domains: renewal date and current price, auto-renew, renew now and transfer out" });
});
