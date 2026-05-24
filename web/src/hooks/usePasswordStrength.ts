import { useCallback, useRef, useState } from "react";

interface ZxcvbnResult {
    score: 0 | 1 | 2 | 3 | 4;
    feedback: { warning: string | null; suggestions: string[] };
}

interface StrengthResult {
    score: 0 | 1 | 2 | 3 | 4;
    warning: string;
    suggestions: string[];
}

const empty: StrengthResult = { score: 0, warning: "", suggestions: [] };

// zxcvbn's actual signature includes optional userInputs; we widen
// the ref to `unknown` and narrow at call site so TS doesn't complain
// about the upstream optional parameter.
type ZxcvbnFn = (pw: string, userInputs?: (string | number)[]) => ZxcvbnResult;

export function usePasswordStrength() {
    const zxcvbnRef = useRef<ZxcvbnFn | null>(null);
    const [loading, setLoading] = useState(false);

    const evaluate = useCallback(async (password: string): Promise<StrengthResult> => {
        if (!password) return empty;

        if (!zxcvbnRef.current) {
            setLoading(true);
            const [{ zxcvbn, zxcvbnOptions }, common, en] = await Promise.all([
                import("@zxcvbn-ts/core"),
                import("@zxcvbn-ts/language-common"),
                import("@zxcvbn-ts/language-en"),
            ]);
            zxcvbnOptions.setOptions({
                translations: en.translations,
                graphs: common.adjacencyGraphs,
                dictionary: {
                    ...common.dictionary,
                    ...en.dictionary,
                },
            });
            zxcvbnRef.current = zxcvbn as ZxcvbnFn;
            setLoading(false);
        }

        const fn = zxcvbnRef.current;
        if (!fn) return empty;
        const result = fn(password);
        return {
            score: result.score,
            warning: result.feedback.warning ?? "",
            suggestions: result.feedback.suggestions,
        };
    }, []);

    return { evaluate, loading };
}
