export const WEBSITE_URL = "https://warmbly.com";
export const APP_URL = import.meta.env.VITE_APP_URL!;
export const API_URL = import.meta.env.VITE_API_URL!;
export const TRACKING_DOMAIN = import.meta.env.VITE_TRACKING_DOMAIN!;
export const HUMAN_VERIFICATION_FAIL = "We couldn’t verify you’re human. Please try the security check again or reload the page.";
export const PASSWORD_FAIL = "The password must be at least 8 characters long and contain both uppercase and lowercase letters, as well as a number."
export const TOKEN_KEY = "auth_token";
export const DEFAULT_PAGINATION_LIMIT = 50;
export const SAVING = "Saving...";
export const REORDERING = "Reordering...";
export const SUCCESS = "Changes successfully changed.";
export const DELETING = "Deleting...";
export const DELETED = "Successfully deleted.";
export const REMOVING = "Removing...";
export const REMOVED = "Successfully removed.";
export const CREATING = "Creating...";
export const CREATED = "Successfully created."
export const ADDING = "Adding..."
export const ADDED = "Successfully added."

// MAILBOX
export const OUTLOOK_BOX_AUTH = API_URL + "/emails/outlook/login";
export const GOOGLE_BOX_AUTH = API_URL + "/emails/google/login";

export function PopupCenter(url: string, title: string) {
  const dualScreenLeft = window.screenLeft ?? window.screenX;
  const dualScreenTop = window.screenTop ?? window.screenY;

  const width =
    window.innerWidth ?? document.documentElement.clientWidth ?? screen.width;

  const height =
    window.innerHeight ??
    document.documentElement.clientHeight ??
    screen.height;

  const systemZoom = width / window.screen.availWidth;

  const left = (width - 500) / 2 / systemZoom + dualScreenLeft;
  const top = (height - 550) / 2 / systemZoom + dualScreenTop;

  const newWindow = window.open(
    url,
    title,
    `width=${500 / systemZoom},height=${550 / systemZoom
    },top=${top},left=${left}`
  );

  newWindow?.focus();
};
