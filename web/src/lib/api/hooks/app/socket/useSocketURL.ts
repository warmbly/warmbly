
import getSocket from "@/lib/api/client/app/socket/getSocket";
import { useMutation } from "@tanstack/react-query";

export default function useSocketURL() {
    return useMutation({
        mutationFn: () => getSocket(),
    })
}
