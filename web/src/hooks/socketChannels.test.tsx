import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, act, screen } from '@testing-library/react'
import React, { useState } from 'react'
import SocketProvider from './SocketProvider'
import { useChannel, useChannelEvent } from './context/socket'
import { installFakeSocket, freezeJitter, type SocketEnv } from './socketTestHarness'
import getSocket from '@/lib/api/client/app/socket/getSocket'

vi.mock('@/lib/api/client/app/socket/getSocket', () => ({
    default: vi.fn(async () => ({ url: 'ws://localhost:4000/socket/websocket?token=test' })),
}))

const TOPIC = 'campaign:11111111-1111-1111-1111-111111111111'

let env: SocketEnv

/** Mounts the provider and lets the socket finish opening. */
async function mount(ui: React.ReactNode) {
    const result = render(<SocketProvider>{ui}</SocketProvider>)
    await act(async () => {
        await vi.advanceTimersByTimeAsync(10)
    })
    return result
}

async function tick(ms: number) {
    await act(async () => {
        await vi.advanceTimersByTimeAsync(ms)
    })
}

function Panel({ topic = TOPIC, onEvent }: { topic?: string; onEvent?: () => void }) {
    const channel = useChannel(topic)
    useChannelEvent(topic, 'EMAIL_SENT', () => onEvent?.())
    return <span data-testid="state">{channel.state}</span>
}

beforeEach(() => {
    vi.mocked(getSocket).mockClear()
    vi.useFakeTimers()
    freezeJitter(0)
    env = installFakeSocket()
})

afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
})

it('retries the direct gateway if an existing API proxy cannot upgrade WebSockets', async () => {
    await mount(<Panel />)
    expect(getSocket).toHaveBeenLastCalledWith(true)
    await act(async () => {
        const socket = env.instances[0]
        socket.readyState = 3
        socket.onclose?.({ wasClean: false })
    })
    await tick(1000)
    expect(getSocket).toHaveBeenLastCalledWith(false)
    expect(env.instances).toHaveLength(2)
})

describe('channel subscription lifecycle', () => {
    it('joins once and stays joined across unrelated re-renders', async () => {
        // Regression: the provider's context value changed identity on every
        // render, and channelStates re-rendered it on every join, so the effect
        // that owns the subscription left and rejoined the channel forever.
        let bump: () => void = () => {}
        function Harness() {
            const [, setN] = useState(0)
            bump = () => setN((n) => n + 1)
            return <Panel />
        }

        await mount(<Harness />)
        env.ackJoin(env.lastJoin(TOPIC))
        await tick(1)

        for (let i = 0; i < 10; i++) {
            await act(async () => {
                bump()
            })
        }

        expect(env.joins(TOPIC)).toHaveLength(1)
        expect(env.leaves(TOPIC)).toHaveLength(0)
    })

    it('renders the joined state once the server acks', async () => {
        await mount(<Panel />)
        expect(screen.getByTestId('state').textContent).toBe('joining')

        await act(async () => {
            env.ackJoin(env.lastJoin(TOPIC))
        })

        expect(screen.getByTestId('state').textContent).toBe('joined')
    })

    it('leaves the channel when its last holder unmounts', async () => {
        // The panel goes away (navigating off the campaign), the socket stays.
        function Wrapper({ show }: { show: boolean }) {
            return show ? <Panel /> : null
        }

        const { rerender } = await mount(<Wrapper show />)
        env.ackJoin(env.lastJoin(TOPIC))
        await tick(1)

        await act(async () => {
            rerender(
                <SocketProvider>
                    <Wrapper show={false} />
                </SocketProvider>
            )
        })

        expect(env.leaves(TOPIC)).toHaveLength(1)
    })
})

describe('two surfaces holding the same topic', () => {
    /** Holds the join without rendering anything. */
    function Joiner() {
        useChannel(TOPIC)
        return null
    }

    /** Listens on the topic without holding the join. */
    function Listener({ onEvent }: { onEvent: () => void }) {
        useChannelEvent(TOPIC, 'EMAIL_SENT', () => onEvent())
        return null
    }

    it('joins once and keeps the channel while the second holder remains', async () => {
        const onA = vi.fn()
        const onB = vi.fn()
        function Wrapper({ showA }: { showA: boolean }) {
            return (
                <>
                    {showA ? <Panel onEvent={onA} /> : null}
                    <Panel onEvent={onB} />
                </>
            )
        }

        const { rerender } = await mount(<Wrapper showA />)
        expect(env.joins(TOPIC)).toHaveLength(1)
        env.ackJoin(env.lastJoin(TOPIC))
        await tick(1)

        // The first holder goes away (the list row scrolls out, the drawer stays).
        await act(async () => {
            rerender(
                <SocketProvider>
                    <Wrapper showA={false} />
                </SocketProvider>
            )
        })

        expect(env.leaves(TOPIC)).toHaveLength(0)
        await act(async () => {
            env.pushEvent(TOPIC, 'EMAIL_SENT', {})
        })
        expect(onB).toHaveBeenCalledTimes(1)
        expect(onA).not.toHaveBeenCalled()
    })

    it('leaves exactly once when the last holder goes', async () => {
        function Wrapper({ holders }: { holders: number }) {
            return (
                <>
                    {Array.from({ length: holders }, (_, i) => (
                        <Joiner key={i} />
                    ))}
                </>
            )
        }

        const { rerender } = await mount(<Wrapper holders={3} />)
        env.ackJoin(env.lastJoin(TOPIC))
        await tick(1)
        expect(env.joins(TOPIC)).toHaveLength(1)

        for (const holders of [2, 1, 0]) {
            await act(async () => {
                rerender(
                    <SocketProvider>
                        <Wrapper holders={holders} />
                    </SocketProvider>
                )
            })
        }

        expect(env.leaves(TOPIC)).toHaveLength(1)
    })

    it('survives a drop while both hold it, then a single holder leaving', async () => {
        // A campaign row and its drawer both hold the topic across a blip.
        const onRow = vi.fn()
        const onDrawer = vi.fn()
        function Wrapper({ drawer }: { drawer: boolean }) {
            return (
                <>
                    <Panel onEvent={onRow} />
                    {drawer ? <Panel onEvent={onDrawer} /> : null}
                </>
            )
        }

        const { rerender } = await mount(<Wrapper drawer />)
        env.ackJoin(env.lastJoin(TOPIC))
        await tick(1)

        // The socket dies; the provider rejoins every topic still wanted.
        await act(async () => {
            env.instances[env.instances.length - 1].close()
        })
        await tick(500)
        expect(env.joins(TOPIC)).toHaveLength(2)
        await act(async () => {
            env.ackJoin(env.lastJoin(TOPIC))
        })

        await act(async () => {
            rerender(
                <SocketProvider>
                    <Wrapper drawer={false} />
                </SocketProvider>
            )
        })

        expect(env.leaves(TOPIC)).toHaveLength(0)
        await act(async () => {
            env.pushEvent(TOPIC, 'EMAIL_SENT', {})
        })
        expect(onRow).toHaveBeenCalledTimes(1)
        expect(onDrawer).not.toHaveBeenCalled()
        expect(screen.getByTestId('state').textContent).toBe('joined')
    })

    it("keeps a listener's handler when a different surface leaves the topic", async () => {
        // Regression: leaveChannel dropped the entry, and with it every other
        // subscriber's handlers on that topic.
        const onEvent = vi.fn()
        function Wrapper({ joined }: { joined: boolean }) {
            return (
                <>
                    <Listener onEvent={onEvent} />
                    {joined ? <Joiner /> : null}
                </>
            )
        }

        const { rerender } = await mount(<Wrapper joined />)
        env.ackJoin(env.lastJoin(TOPIC))
        await tick(1)

        await act(async () => {
            rerender(
                <SocketProvider>
                    <Wrapper joined={false} />
                </SocketProvider>
            )
        })
        expect(env.leaves(TOPIC)).toHaveLength(1)

        await act(async () => {
            rerender(
                <SocketProvider>
                    <Wrapper joined />
                </SocketProvider>
            )
        })
        await act(async () => {
            env.ackJoin(env.lastJoin(TOPIC))
        })
        await act(async () => {
            env.pushEvent(TOPIC, 'EMAIL_SENT', {})
        })

        expect(onEvent).toHaveBeenCalledTimes(1)
    })
})

describe('a refused join', () => {
    it('retries a throttled join when the server says budget is back', async () => {
        await mount(<Panel />)

        await act(async () => {
            env.refuseJoin(env.lastJoin(TOPIC), {
                code: 4007,
                reason: 'rate_limited',
                category: 'ws_join',
                retry_after_ms: 5000,
            })
        })

        expect(screen.getByTestId('state').textContent).toBe('errored')
        expect(env.joins(TOPIC)).toHaveLength(1)

        // Nothing before the hint elapses.
        await tick(4000)
        expect(env.joins(TOPIC)).toHaveLength(1)

        await tick(1500)
        expect(env.joins(TOPIC)).toHaveLength(2)

        await act(async () => {
            env.ackJoin(env.lastJoin(TOPIC))
        })
        expect(screen.getByTestId('state').textContent).toBe('joined')
    })

    it('never retries a refusal that will not change', async () => {
        await mount(<Panel />)

        await act(async () => {
            env.refuseJoin(env.lastJoin(TOPIC), { code: 4010, reason: 'not_a_member' })
        })

        await tick(120_000)

        expect(env.joins(TOPIC)).toHaveLength(1)
        expect(screen.getByTestId('state').textContent).toBe('errored')
    })

    it('gives up on a throttled join rather than retrying forever', async () => {
        await mount(<Panel />)

        // Refuse every attempt.
        for (let i = 0; i < 20; i++) {
            await act(async () => {
                env.refuseJoin(env.lastJoin(TOPIC), {
                    reason: 'rate_limited',
                    retry_after_ms: 1000,
                })
            })
            await tick(2000)
        }

        expect(env.joins(TOPIC).length).toBeLessThanOrEqual(9)
    })

    it('clamps an absurd retry hint', async () => {
        await mount(<Panel />)

        await act(async () => {
            env.refuseJoin(env.lastJoin(TOPIC), {
                reason: 'rate_limited',
                retry_after_ms: 999_999_999,
            })
        })

        await tick(61_000)
        expect(env.joins(TOPIC)).toHaveLength(2)
    })

    it('treats a missing retry hint as the floor', async () => {
        await mount(<Panel />)

        await act(async () => {
            env.refuseJoin(env.lastJoin(TOPIC), { reason: 'rate_limited' })
        })

        await tick(1200)
        expect(env.joins(TOPIC)).toHaveLength(2)
    })
})

describe('a channel that crashes server-side', () => {
    /** Crash the channel, then report how long the client waited to rejoin. */
    async function crashAndMeasureDelay(): Promise<number> {
        const before = env.joins(TOPIC).length
        await act(async () => {
            env.deliver({ topic: TOPIC, event: 'phx_error', payload: {} })
        })

        let waited = 0
        while (env.joins(TOPIC).length === before && waited < 90_000) {
            await tick(100)
            waited += 100
        }
        return waited
    }

    it('backs off further on each crash instead of retrying once a second', async () => {
        await mount(<Panel />)
        env.ackJoin(env.lastJoin(TOPIC))
        await tick(1)

        // A crash loop always joins successfully before it crashes again, so a
        // backoff that reset on a successful join would stay flat at 1s forever.
        const delays: number[] = []
        for (let i = 0; i < 3; i++) {
            delays.push(await crashAndMeasureDelay())
            env.ackJoin(env.lastJoin(TOPIC))
            await tick(1)
        }

        expect(delays).toEqual([1000, 2000, 5000])
    })

    it('starts the backoff over once the channel has been healthy for a while', async () => {
        await mount(<Panel />)
        env.ackJoin(env.lastJoin(TOPIC))
        await tick(1)

        expect(await crashAndMeasureDelay()).toBe(1000)
        env.ackJoin(env.lastJoin(TOPIC))

        // Quiet for well over the reset window.
        await tick(45_000)

        expect(await crashAndMeasureDelay()).toBe(1000)
    })

    it('keeps event handlers alive across a rejoin', async () => {
        const onEvent = vi.fn()
        await mount(<Panel onEvent={onEvent} />)
        env.ackJoin(env.lastJoin(TOPIC))
        await tick(1)

        await act(async () => {
            env.deliver({ topic: TOPIC, event: 'phx_error', payload: {} })
        })
        await tick(2000)

        await act(async () => {
            env.ackJoin(env.lastJoin(TOPIC))
        })
        await act(async () => {
            env.pushEvent(TOPIC, 'EMAIL_SENT', {})
        })

        expect(onEvent).toHaveBeenCalledTimes(1)
    })

    it('does not rejoin a channel the app has left', async () => {
        function Wrapper({ show }: { show: boolean }) {
            return show ? <Panel /> : null
        }

        const { rerender } = await mount(<Wrapper show />)
        env.ackJoin(env.lastJoin(TOPIC))
        await tick(1)

        await act(async () => {
            env.deliver({ topic: TOPIC, event: 'phx_error', payload: {} })
            rerender(
                <SocketProvider>
                    <Wrapper show={false} />
                </SocketProvider>
            )
        })
        const joinsAfterLeave = env.joins(TOPIC).length

        await tick(60_000)
        expect(env.joins(TOPIC)).toHaveLength(joinsAfterLeave)
    })
})
