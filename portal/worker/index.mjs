// The portal service's entry module. workerd treats every export of the main module as an entrypoint, so the
// handler lives in portal.mjs and only the default export is here.
import { handle } from "./portal.mjs";

export default { fetch: handle };
