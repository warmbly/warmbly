import type Token from "./Token";

// LoginConfirm now returns either the token pair (success) or a 2FA challenge.
export interface LoginResult extends Partial<Token> {
    two_fa_required?: boolean;
    pending_token?: string;
    expires_in?: number;
}
