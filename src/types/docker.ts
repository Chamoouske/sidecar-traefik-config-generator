/**
 * Tipos baseados na API Dockerode para representar entidades do Docker Swarm.
 */

export interface SwarmNode {
    id: string;
    hostname: string;
    ip: string;
    availability: string;
    status: string;
}

export interface SwarmTask {
    id: string;
    nodeId: string;
    serviceId: string;
    status: string;
    desiredState: string;
    slot: number;
}

export interface SwarmService {
    id: string;
    name: string;
    labels: Record<string, string>;
    ports?: Array<{ published: number; target: number }>;
    image: string;
    replicas: number;
}

export interface ServiceEndpoint {
    nodeId: string;
    nodeHostname: string;
    nodeIp: string;
    taskStatus: string;
    taskId: string;
}

export interface DiscoveredService {
    serviceName: string;
    serviceId: string;
    labels: Record<string, string>;
    endpoints: ServiceEndpoint[];
}
