import { useMemo } from 'react';
import { useCampaignChannel, type ActivityItem } from '@/hooks/useCampaignChannel';

interface TaskPreviewProps {
    campaignId: string;
    campaignStatus?: string;
}

// Status badge colors
const statusColors: Record<string, string> = {
    active: 'bg-green-100 text-green-800',
    paused: 'bg-yellow-100 text-yellow-800',
    draft: 'bg-gray-100 text-gray-800',
    completed: 'bg-blue-100 text-blue-800',
};

// Activity type colors
const activityColors: Record<string, string> = {
    sent: 'text-blue-600',
    opened: 'text-green-600',
    clicked: 'text-purple-600',
    replied: 'text-indigo-600',
    bounced: 'text-orange-600',
    failed: 'text-red-600',
};

// Activity type icons
const activityIcons: Record<string, string> = {
    sent: 'M3 8l7.89 5.26a2 2 0 002.22 0L21 8M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z',
    opened: 'M15 12a3 3 0 11-6 0 3 3 0 016 0z M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z',
    clicked: 'M15 15l-2 5L9 9l11 4-5 2zm0 0l5 5M7.188 2.239l.777 2.897M5.136 7.965l-2.898-.777M13.95 4.05l-2.122 2.122m-5.657 5.656l-2.12 2.122',
    replied: 'M3 10h10a8 8 0 018 8v2M3 10l6 6m-6-6l6-6',
    bounced: 'M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z',
    failed: 'M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z',
};

function formatTime(date: Date): string {
    return date.toLocaleTimeString('en-US', {
        hour: '2-digit',
        minute: '2-digit',
    });
}

function ActivityIcon({ type }: { type: string }) {
    const path = activityIcons[type] || activityIcons.sent;
    return (
        <svg
            className={`w-4 h-4 ${activityColors[type] || 'text-gray-600'}`}
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
        >
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d={path} />
        </svg>
    );
}

function ActivityFeedItem({ activity }: { activity: ActivityItem }) {
    return (
        <div className="flex items-start gap-3 py-2 px-3 hover:bg-gray-50 rounded-lg transition-colors">
            <div className="mt-0.5">
                <ActivityIcon type={activity.type} />
            </div>
            <div className="flex-1 min-w-0">
                <p className="text-sm text-gray-900 truncate">{activity.message}</p>
                <p className="text-xs text-gray-500">{formatTime(activity.timestamp)}</p>
            </div>
        </div>
    );
}

export default function TaskPreview({ campaignId, campaignStatus: initialStatus }: TaskPreviewProps) {
    const {
        isConnected,
        channelState,
        campaignStatus: realtimeStatus,
        taskProgress,
        activities,
    } = useCampaignChannel(campaignId);

    // Use realtime status if available, otherwise fall back to initial
    const currentStatus = realtimeStatus?.status || initialStatus || 'draft';

    // Calculate estimated time remaining
    const estimatedTimeRemaining = useMemo(() => {
        if (!taskProgress || taskProgress.processed_count === 0) return null;

        const remaining = taskProgress.total_contacts - taskProgress.processed_count;
        if (remaining <= 0) return null;

        // Estimate based on recent activity (assuming ~1 email per minute average)
        const minutes = remaining;
        if (minutes < 60) return `~${minutes} min`;
        const hours = Math.floor(minutes / 60);
        const mins = minutes % 60;
        return `~${hours}h ${mins}m`;
    }, [taskProgress]);

    return (
        <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
            {/* Header */}
            <div className="px-4 py-3 border-b border-gray-100 flex items-center justify-between">
                <div className="flex items-center gap-3">
                    <h3 className="text-sm font-medium text-gray-900">Live Preview</h3>
                    <span className={`px-2 py-0.5 text-xs font-medium rounded-full ${statusColors[currentStatus] || statusColors.draft}`}>
                        {currentStatus.charAt(0).toUpperCase() + currentStatus.slice(1)}
                    </span>
                </div>
                <div className="flex items-center gap-2">
                    <span className={`w-2 h-2 rounded-full ${isConnected ? 'bg-green-500' : 'bg-gray-300'}`} />
                    <span className="text-xs text-gray-500">
                        {isConnected ? 'Connected' : channelState === 'joining' ? 'Connecting...' : 'Disconnected'}
                    </span>
                </div>
            </div>

            {/* Current Task Card */}
            {taskProgress && currentStatus === 'active' && (
                <div className="px-4 py-3 border-b border-gray-100 bg-gray-50">
                    <div className="flex items-start gap-3">
                        {/* Contact Avatar */}
                        <div className="w-10 h-10 rounded-full bg-gradient-to-br from-blue-500 to-purple-600 flex items-center justify-center text-white font-medium text-sm">
                            {taskProgress.contact_name?.[0]?.toUpperCase() || taskProgress.contact_email?.[0]?.toUpperCase() || '?'}
                        </div>
                        <div className="flex-1 min-w-0">
                            <p className="text-sm font-medium text-gray-900 truncate">
                                {taskProgress.contact_name || 'Unknown Contact'}
                            </p>
                            <p className="text-xs text-gray-500 truncate">{taskProgress.contact_email}</p>
                            {taskProgress.sequence_name && (
                                <p className="text-xs text-blue-600 mt-1">
                                    {taskProgress.sequence_name}
                                    {taskProgress.sequence_index > 0 && ` (Step ${taskProgress.sequence_index})`}
                                </p>
                            )}
                        </div>
                        <div className="text-right">
                            <span className={`inline-flex items-center px-2 py-0.5 text-xs font-medium rounded ${
                                taskProgress.status === 'active' ? 'bg-blue-100 text-blue-700' :
                                taskProgress.status === 'completed' ? 'bg-green-100 text-green-700' :
                                taskProgress.status === 'failed' ? 'bg-red-100 text-red-700' :
                                'bg-gray-100 text-gray-700'
                            }`}>
                                {taskProgress.status === 'active' ? 'Sending...' : taskProgress.status}
                            </span>
                        </div>
                    </div>
                </div>
            )}

            {/* Progress Section */}
            <div className="px-4 py-3 border-b border-gray-100">
                <div className="flex items-center justify-between mb-2">
                    <span className="text-sm text-gray-600">Progress</span>
                    <span className="text-sm font-medium text-gray-900">
                        {taskProgress?.progress ?? 0}%
                    </span>
                </div>
                <div className="h-2 bg-gray-100 rounded-full overflow-hidden">
                    <div
                        className="h-full bg-gradient-to-r from-blue-500 to-purple-500 rounded-full transition-all duration-500"
                        style={{ width: `${taskProgress?.progress ?? 0}%` }}
                    />
                </div>
                <div className="flex items-center justify-between mt-2">
                    <span className="text-xs text-gray-500">
                        {taskProgress?.processed_count ?? 0} of {taskProgress?.total_contacts ?? 0} contacts
                    </span>
                    {estimatedTimeRemaining && (
                        <span className="text-xs text-gray-500">
                            {estimatedTimeRemaining} remaining
                        </span>
                    )}
                </div>
            </div>

            {/* Activity Feed */}
            <div className="max-h-64 overflow-y-auto">
                {activities.length > 0 ? (
                    <div className="divide-y divide-gray-50">
                        {activities.map((activity) => (
                            <ActivityFeedItem key={activity.id} activity={activity} />
                        ))}
                    </div>
                ) : (
                    <div className="px-4 py-8 text-center">
                        <svg
                            className="w-8 h-8 mx-auto text-gray-300 mb-2"
                            fill="none"
                            stroke="currentColor"
                            viewBox="0 0 24 24"
                        >
                            <path
                                strokeLinecap="round"
                                strokeLinejoin="round"
                                strokeWidth={1.5}
                                d="M13 10V3L4 14h7v7l9-11h-7z"
                            />
                        </svg>
                        <p className="text-sm text-gray-500">
                            {currentStatus === 'active'
                                ? 'Waiting for activity...'
                                : 'Start the campaign to see live updates'}
                        </p>
                    </div>
                )}
            </div>
        </div>
    );
}
