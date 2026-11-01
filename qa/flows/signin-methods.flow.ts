import { expect, test } from "../lib/proof.ts";

// Requires ../fixtures/signin-methods.sql applied to this worktree's rich-seed database.
const FIXTURES = process.env.QA_SIGNIN_METHODS_FIXTURES === "1";

test.describe("sign-in methods", () => {
  test.skip(!FIXTURES, "Apply qa/fixtures/signin-methods.sql to this stack's database and set QA_SIGNIN_METHODS_FIXTURES=1.");

  test("move an Apple and Google account to email sign-in from the security page", async ({ page, proof }) => {
    await page.goto("/app/settings/security");
    const setPassword = page.getByRole("button", { name: "Email me a link" });
    await expect(setPassword).toBeVisible();
    await expect(page.getByText("Apple", { exact: true })).toBeVisible();
    await proof.chapter("Sign-in methods", "An account created through Apple and Google, with no password yet");
    await proof.shot("security-no-password", { caption: "No password yet: email a link to set one; Apple and Google are listed with the address each uses" });

    await setPassword.click();
    await expect(page.getByText(/Open the link we sent to/)).toBeVisible();
    await proof.shot("password-link-sent", { caption: "The link goes to the account's own address" });

    await page.getByRole("button", { name: "Remove Google" }).click();
    const confirm = page.getByRole("alertdialog");
    await expect(confirm.getByText(/Stop signing in with Google/)).toBeVisible();
    await proof.shot("remove-google-confirm", { caption: "Removing a provider says what it does first" });
    await confirm.getByRole("button", { name: "Confirm" }).click();
    await expect(page.getByRole("button", { name: "Remove Google" })).toBeHidden();
    await proof.shot("google-removed", { caption: "Google is gone; Apple still signs in" });

    await page.getByRole("button", { name: "Remove Apple" }).click();
    await page.getByRole("alertdialog").getByRole("button", { name: "Confirm" }).click();
    await expect(page.getByText(/This is the only way you sign in/)).toBeVisible();
    await expect(page.getByRole("button", { name: "Remove Apple" })).toBeVisible();
    await proof.dwell();
    await proof.shot("last-method-refused", { caption: "The last way in cannot be removed until a password or passkey exists" });
  });
});
