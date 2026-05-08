import { describe, it, expect, beforeEach } from 'vitest';
import { ServiceLocalityService } from '../services/ServiceLocalityService';
import { AppConfig } from '../types/config';
import { DiscoveredService } from '../types/docker';

function createConfig(nodeId: string): AppConfig {
  return {
    mode: 'all',
    node: { hostname: 'node1', ip: '192.168.1.1', nodeId },
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
}

function createService(endpointNodeIds: string[]): DiscoveredService {
  return {
    serviceName: 'test-app',
    serviceId: 'abc123',
    labels: {
      'federation.enable': 'true',
      'federation.host': 'test.local',
      'federation.port': '3000',
    },
    endpoints: endpointNodeIds.map((nodeId, i) => ({
      nodeId,
      nodeHostname: `node-${i}`,
      nodeIp: `192.168.1.${i + 1}`,
      taskStatus: 'running',
      taskId: `task-${i}`,
    })),
  };
}

describe('ServiceLocalityService', () => {
  describe('isLocal', () => {
    it('should return true when service has local endpoint', () => {
      const service = createService(['local-node', 'remote-node']);
      const config = createConfig('local-node');
      const locality = new ServiceLocalityService(config);
      expect(locality.isLocal(service)).toBe(true);
    });

    it('should return false when service has no local endpoint', () => {
      const service = createService(['remote-node-1', 'remote-node-2']);
      const config = createConfig('local-node');
      const locality = new ServiceLocalityService(config);
      expect(locality.isLocal(service)).toBe(false);
    });

    it('should return false when endpoints array is empty', () => {
      const service = createService([]);
      const config = createConfig('local-node');
      const locality = new ServiceLocalityService(config);
      expect(locality.isLocal(service)).toBe(false);
    });

    it('should return true when multiple local endpoints exist', () => {
      const service = createService(['local-node', 'local-node', 'remote-node']);
      const config = createConfig('local-node');
      const locality = new ServiceLocalityService(config);
      expect(locality.isLocal(service)).toBe(true);
    });
  });

  describe('getLocalEndpoints', () => {
    it('should return only local endpoints', () => {
      const service = createService(['local-node', 'remote-node']);
      const config = createConfig('local-node');
      const locality = new ServiceLocalityService(config);
      const local = locality.getLocalEndpoints(service);
      expect(local).toHaveLength(1);
      expect(local[0].url).toContain('192.168.1.1:3000');
    });

    it('should return empty array when no local endpoints', () => {
      const service = createService(['remote-node']);
      const config = createConfig('local-node');
      const locality = new ServiceLocalityService(config);
      expect(locality.getLocalEndpoints(service)).toHaveLength(0);
    });

    it('should return all local endpoints when multiple exist', () => {
      const service = createService(['local-node', 'remote-node', 'local-node']);
      const config = createConfig('local-node');
      const locality = new ServiceLocalityService(config);
      const local = locality.getLocalEndpoints(service);
      expect(local).toHaveLength(2);
    });
  });

  describe('getRemoteEndpoints', () => {
    it('should return only remote endpoints', () => {
      const service = createService(['local-node', 'remote-node']);
      const config = createConfig('local-node');
      const locality = new ServiceLocalityService(config);
      const remote = locality.getRemoteEndpoints(service);
      expect(remote).toHaveLength(1);
      expect(remote[0].url).toContain('192.168.1.2:3000');
    });

    it('should return empty array when no remote endpoints', () => {
      const service = createService(['local-node']);
      const config = createConfig('local-node');
      const locality = new ServiceLocalityService(config);
      expect(locality.getRemoteEndpoints(service)).toHaveLength(0);
    });
  });

  describe('getWeightedServers', () => {
    it('should weight local higher than remote', () => {
      const service = createService(['local-node', 'remote-node']);
      const config = createConfig('local-node');
      const locality = new ServiceLocalityService(config);
      const servers = locality.getWeightedServers(service);
      const localServer = servers.find((s) => s.url.includes('192.168.1.1'));
      const remoteServer = servers.find((s) => s.url.includes('192.168.1.2'));
      expect(localServer?.weight).toBe(10);
      expect(remoteServer?.weight).toBe(1);
    });

    it('should return server without weight when only one local replica', () => {
      const service = createService(['local-node']);
      const config = createConfig('local-node');
      const locality = new ServiceLocalityService(config);
      const servers = locality.getWeightedServers(service);
      expect(servers).toHaveLength(1);
      expect(servers[0].weight).toBeUndefined();
    });

    it('should not strip weight when local has multiple replicas and no remote', () => {
      const service = createService(['local-node', 'local-node']);
      const config = createConfig('local-node');
      const locality = new ServiceLocalityService(config);
      const servers = locality.getWeightedServers(service);
      expect(servers).toHaveLength(2);
      expect(servers[0].weight).toBe(10);
      expect(servers[1].weight).toBe(10);
    });

    it('should use port from federation labels', () => {
      const service = createService(['local-node', 'remote-node']);
      service.labels['federation.port'] = '8080';
      const config = createConfig('local-node');
      const locality = new ServiceLocalityService(config);
      const servers = locality.getWeightedServers(service);
      servers.forEach((s) => {
        expect(s.url).toContain(':8080');
      });
    });

    it('should fallback to port 80 when no federation.port label', () => {
      const service = createService(['local-node', 'remote-node']);
      delete service.labels['federation.port'];
      const config = createConfig('local-node');
      const locality = new ServiceLocalityService(config);
      const servers = locality.getWeightedServers(service);
      servers.forEach((s) => {
        expect(s.url).toContain(':80');
      });
    });

    it('should fallback to port 80 when federation.port is invalid', () => {
      const service = createService(['local-node', 'remote-node']);
      service.labels['federation.port'] = 'invalid';
      const config = createConfig('local-node');
      const locality = new ServiceLocalityService(config);
      const servers = locality.getWeightedServers(service);
      servers.forEach((s) => {
        expect(s.url).toContain(':80');
      });
    });
  });

  describe('constructor', () => {
    it('should use config nodeId for locality decisions', () => {
      const service = createService(['node-a']);
      const config = createConfig('node-a');
      const locality = new ServiceLocalityService(config);
      expect(locality.isLocal(service)).toBe(true);
    });
  });
});
