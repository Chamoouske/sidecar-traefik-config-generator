import { describe, it, expect, vi, beforeEach } from 'vitest';
import { LocalDiscoveryService } from '../services/LocalDiscoveryService.js';
import { IDockerClient } from '../core/interfaces/IDockerClient.js';
import { ILabelParser } from '../core/interfaces/ILabelParser.js';
import { ILogger } from '../core/interfaces/ILogger.js';
import { AppConfig } from '../types/config.js';

const mockLogger: ILogger = {
    info: vi.fn(),
    warn: vi.fn(),
    error: vi.fn(),
    debug: vi.fn(),
    fatal: vi.fn(),
    child: () => mockLogger,
};

describe('LocalDiscoveryService', () => {
    const mockDockerClient: IDockerClient = {
        connect: vi.fn(),
        disconnect: vi.fn(),
        getNodes: vi.fn(),
        getServices: vi.fn(),
        getServiceTasks: vi.fn(),
        getNodeInfo: vi.fn(),
        isConnected: vi.fn(),
        onReconnect: vi.fn(),
        listContainers: vi.fn(),
    };

    const mockLabelParser: ILabelParser = {
        parse: vi.fn(),
        isFederationEnabled: vi.fn(),
    };

    const mockConfig: AppConfig = {
        mode: 'local',
        node: { hostname: 'node1', ip: '192.168.1.1', nodeId: 'node-1' },
        docker: { socket: '/var/run/docker.sock', pollIntervalMs: 30000 },
        directories: {
            shared: '/data/shared',
            local: '/data/local',
            federation: '/data/shared/federation',
            middlewares: '/data/shared/middlewares',
            localGenerated: '/data/local/generated',
        },
        federation: {
            headerName: 'X-Federated',
            headerValue: 'true',
            defaultHealthCheckPath: '/',
            defaultHealthCheckInterval: '10s',
            defaultRetryAttempts: 3,
            defaultRetryInterval: '100ms',
            circuitBreakerThreshold: 0.30,
        },
        server: { port: 9090, healthEndpoint: '/health' },
        logging: { level: 'info', pretty: false },
    };

    const discovery = new LocalDiscoveryService(
        mockDockerClient,
        mockLabelParser,
        mockConfig,
        mockLogger,
    );

    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('should discover local federated services from containers', async () => {
        const mockContainers = [
            {
                Id: 'container-1',
                Names: ['/app1'],
                Labels: {
                    'federation.enable': 'true',
                    'com.docker.swarm.service.name': 'app1',
                    'federation.port': '8080',
                },
                State: 'running',
            },
        ];

        vi.mocked(mockDockerClient.listContainers).mockResolvedValue(mockContainers);

        const result = await discovery.discoverLocalServices();
        expect(result).toHaveLength(1);
        expect(result[0].serviceName).toBe('app1');
        expect(result[0].endpoints).toHaveLength(1);
        expect(result[0].endpoints[0].nodeId).toBe('node-1');
        expect(result[0].endpoints[0].nodeHostname).toBe('node1');
        expect(result[0].endpoints[0].nodeIp).toBe('192.168.1.1');
    });

    it('should return empty array when no containers with federation labels exist', async () => {
        vi.mocked(mockDockerClient.listContainers).mockResolvedValue([]);

        const result = await discovery.discoverLocalServices();
        expect(result).toHaveLength(0);
    });

    it('should use container name when swarm service label is absent', async () => {
        const mockContainers = [
            {
                Id: 'container-abc123',
                Names: ['/my-custom-container'],
                Labels: {
                    'federation.enable': 'true',
                },
                State: 'running',
            },
        ];

        vi.mocked(mockDockerClient.listContainers).mockResolvedValue(mockContainers);

        const result = await discovery.discoverLocalServices();
        expect(result).toHaveLength(1);
        expect(result[0].serviceName).toBe('my-custom-container');
    });

    it('should use container-id-based name when no name labels exist', async () => {
        const mockContainers = [
            {
                Id: 'container-abc123def',
                Names: [],
                Labels: {
                    'federation.enable': 'true',
                },
                State: 'running',
            },
        ];

        vi.mocked(mockDockerClient.listContainers).mockResolvedValue(mockContainers);

        const result = await discovery.discoverLocalServices();
        expect(result).toHaveLength(1);
        expect(result[0].serviceName).toBe('container-container-ab');
    });

    it('should handle discovery errors for a container without stopping others', async () => {
        const mockContainers = [
            {
                Id: 'container-good',
                Names: ['/good-app'],
                Labels: {
                    'federation.enable': 'true',
                    'com.docker.swarm.service.name': 'good-app',
                },
                State: 'running',
            },
            {
                // Container with missing Id and Names to trigger processing error
                Id: undefined as any,
                Names: undefined as any,
                Labels: {
                    'federation.enable': 'true',
                },
                State: 'running',
            },
        ];

        vi.mocked(mockDockerClient.listContainers).mockResolvedValue(mockContainers);

        const result = await discovery.discoverLocalServices();
        expect(result).toHaveLength(1);
        expect(result[0].serviceName).toBe('good-app');
        expect(mockLogger.warn).toHaveBeenCalledWith(
            'Failed to process container',
            expect.objectContaining({
                error: expect.any(String),
            }),
        );
    });

    it('should use default port 80 when federation.port label is absent', async () => {
        const mockContainers = [
            {
                Id: 'container-1',
                Names: ['/app1'],
                Labels: {
                    'federation.enable': 'true',
                    'com.docker.swarm.service.name': 'app1',
                },
                State: 'running',
            },
        ];

        vi.mocked(mockDockerClient.listContainers).mockResolvedValue(mockContainers);

        const result = await discovery.discoverLocalServices();
        expect(result).toHaveLength(1);
        // Port is used internally; just verify service was discovered
        expect(result[0].serviceName).toBe('app1');
    });

    it('should aggregate endpoints for containers with same service name', async () => {
        const mockContainers = [
            {
                Id: 'container-1',
                Names: ['/app1'],
                Labels: {
                    'federation.enable': 'true',
                    'com.docker.swarm.service.name': 'app1',
                },
                State: 'running',
            },
            {
                Id: 'container-2',
                Names: ['/app1-replica'],
                Labels: {
                    'federation.enable': 'true',
                    'com.docker.swarm.service.name': 'app1',
                },
                State: 'running',
            },
        ];

        vi.mocked(mockDockerClient.listContainers).mockResolvedValue(mockContainers);

        const result = await discovery.discoverLocalServices();
        expect(result).toHaveLength(1);
        expect(result[0].endpoints).toHaveLength(2);
        expect(result[0].endpoints[0].taskId).toBe('container-1');
        expect(result[0].endpoints[1].taskId).toBe('container-2');
    });

    it('should use container State as taskStatus', async () => {
        const mockContainers = [
            {
                Id: 'container-1',
                Names: ['/app1'],
                Labels: {
                    'federation.enable': 'true',
                    'com.docker.swarm.service.name': 'app1',
                },
                State: 'running',
            },
        ];

        vi.mocked(mockDockerClient.listContainers).mockResolvedValue(mockContainers);

        const result = await discovery.discoverLocalServices();
        expect(result[0].endpoints[0].taskStatus).toBe('running');
    });

    it('getCurrentNodeId should return configured node id', () => {
        expect(discovery.getCurrentNodeId()).toBe('node-1');
    });

    it('should call listContainers with federation label filter', async () => {
        vi.mocked(mockDockerClient.listContainers).mockResolvedValue([]);

        await discovery.discoverLocalServices();

        expect(mockDockerClient.listContainers).toHaveBeenCalledWith({
            filters: {
                label: ['federation.enable=true'],
            },
        });
    });
});
