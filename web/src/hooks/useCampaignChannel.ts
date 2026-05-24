import { useState, useCallback, useEffect } from 'react';
import { useChannel, useChannelEvent } from './context/socket';

// Task progress event payload
export interface TaskProgressPayload {
    campaign_id: string;
    task_id: string;
    status: 'pending' | 'active' | 'completed' | 'failed';
    contact_id: string;
    contact_email: string;
    contact_name: string;
    sequence_id: string;
    sequence_name: string;
    sequence_index: number;
    progress: number;
    total_contacts: number;
    processed_count: number;
    timestamp: string;
}

// Campaign status event payload
export interface CampaignStatusPayload {
    campaign_id: string;
    status: 'draft' | 'active' | 'paused' | 'completed';
    name?: string;
}

// Activity item for the feed
export interface ActivityItem {
    id: string;
    type: 'sent' | 'opened' | 'clicked' | 'replied' | 'bounced' | 'failed';
    contactEmail: string;
    contactName?: string;
    message: string;
    timestamp: Date;
}

// Hook return type
export interface CampaignChannelState {
    isConnected: boolean;
    channelState: string;
    campaignStatus: CampaignStatusPayload | null;
    taskProgress: TaskProgressPayload | null;
    activities: ActivityItem[];
    clearActivities: () => void;
}

const MAX_ACTIVITIES = 50;

export function useCampaignChannel(campaignId: string): CampaignChannelState {
    const topic = `campaign:${campaignId}`;
    const channel = useChannel(topic);

    const [campaignStatus, setCampaignStatus] = useState<CampaignStatusPayload | null>(null);
    const [taskProgress, setTaskProgress] = useState<TaskProgressPayload | null>(null);
    const [activities, setActivities] = useState<ActivityItem[]>([]);

    // Add activity to feed
    const addActivity = useCallback((activity: ActivityItem) => {
        setActivities((prev) => {
            const newActivities = [activity, ...prev];
            return newActivities.slice(0, MAX_ACTIVITIES);
        });
    }, []);

    // Clear activities
    const clearActivities = useCallback(() => {
        setActivities([]);
    }, []);

    // Handle task progress events
    useChannelEvent(topic, 'task_progress', (payload) => {
        const data = payload as unknown as TaskProgressPayload;
        setTaskProgress(data);

        // Add to activity feed based on status
        if (data.status === 'completed') {
            addActivity({
                id: `${data.task_id}-${Date.now()}`,
                type: 'sent',
                contactEmail: data.contact_email,
                contactName: data.contact_name,
                message: `Email sent to ${data.contact_email}`,
                timestamp: new Date(data.timestamp || Date.now()),
            });
        } else if (data.status === 'failed') {
            addActivity({
                id: `${data.task_id}-${Date.now()}`,
                type: 'failed',
                contactEmail: data.contact_email,
                contactName: data.contact_name,
                message: `Failed to send to ${data.contact_email}`,
                timestamp: new Date(data.timestamp || Date.now()),
            });
        }
    });

    // Handle campaign status changes
    useChannelEvent(topic, 'campaign_updated', (payload) => {
        const data = payload as unknown as CampaignStatusPayload;
        setCampaignStatus(data);
    });

    useChannelEvent(topic, 'campaign_started', (payload) => {
        const data = payload as unknown as CampaignStatusPayload;
        setCampaignStatus({ ...data, status: 'active' });
    });

    useChannelEvent(topic, 'campaign_paused', (payload) => {
        const data = payload as unknown as CampaignStatusPayload;
        setCampaignStatus({ ...data, status: 'paused' });
    });

    useChannelEvent(topic, 'campaign_completed', (payload) => {
        const data = payload as unknown as CampaignStatusPayload;
        setCampaignStatus({ ...data, status: 'completed' });
    });

    // Handle tracking events
    useChannelEvent(topic, 'email_opened', (payload) => {
        const data = payload as { contact_email?: string; contact_id?: string };
        addActivity({
            id: `open-${data.contact_id}-${Date.now()}`,
            type: 'opened',
            contactEmail: data.contact_email || 'Unknown',
            message: `Opened by ${data.contact_email || 'Unknown'}`,
            timestamp: new Date(),
        });
    });

    useChannelEvent(topic, 'email_clicked', (payload) => {
        const data = payload as { contact_email?: string; contact_id?: string; original_url?: string };
        addActivity({
            id: `click-${data.contact_id}-${Date.now()}`,
            type: 'clicked',
            contactEmail: data.contact_email || 'Unknown',
            message: `Click from ${data.contact_email || 'Unknown'}`,
            timestamp: new Date(),
        });
    });

    useChannelEvent(topic, 'email_replied', (payload) => {
        const data = payload as { contact_email?: string; contact_id?: string };
        addActivity({
            id: `reply-${data.contact_id}-${Date.now()}`,
            type: 'replied',
            contactEmail: data.contact_email || 'Unknown',
            message: `Reply from ${data.contact_email || 'Unknown'}`,
            timestamp: new Date(),
        });
    });

    useChannelEvent(topic, 'email_bounced', (payload) => {
        const data = payload as { contact_email?: string; contact_id?: string };
        addActivity({
            id: `bounce-${data.contact_id}-${Date.now()}`,
            type: 'bounced',
            contactEmail: data.contact_email || 'Unknown',
            message: `Bounced: ${data.contact_email || 'Unknown'}`,
            timestamp: new Date(),
        });
    });

    // Reset state when campaign changes
    useEffect(() => {
        setCampaignStatus(null);
        setTaskProgress(null);
        setActivities([]);
    }, [campaignId]);

    return {
        isConnected: channel.state === 'joined',
        channelState: channel.state,
        campaignStatus,
        taskProgress,
        activities,
        clearActivities,
    };
}

export default useCampaignChannel;
