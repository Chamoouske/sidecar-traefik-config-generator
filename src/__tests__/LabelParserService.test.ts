import { describe, it, expect } from 'vitest';
import { LabelParserService } from '../services/LabelParserService';

describe('LabelParserService', () => {
  const parser = new LabelParserService();

  it('should parse valid federation labels', () => {
    const labels = {
      'federation.enable': 'true',
      'federation.host': 'app.local',
      'federation.port': '3000',
    };
    const result = parser.parse(labels);
    expect(result).not.toBeNull();
    expect(result!.enabled).toBe(true);
    expect(result!.host).toBe('app.local');
    expect(result!.port).toBe(3000);
  });

  it('should return null when federation is not enabled', () => {
    const labels = { 'some.other.label': 'true' };
    expect(parser.parse(labels)).toBeNull();
  });

  it('should return null when federation.enable is false', () => {
    const labels = {
      'federation.enable': 'false',
      'federation.host': 'app.local',
      'federation.port': '3000',
    };
    expect(parser.parse(labels)).toBeNull();
  });

  it('should parse optional labels correctly', () => {
    const labels = {
      'federation.enable': 'true',
      'federation.host': 'app.local',
      'federation.port': '3000',
      'federation.sticky': 'true',
      'federation.retryAttempts': '5',
      'federation.retryInterval': '200ms',
      'federation.circuitBreaker': 'true',
      'federation.healthCheckPath': '/api/health',
      'federation.healthCheckInterval': '15s',
      'federation.localityAware': 'true',
    };
    const result = parser.parse(labels);
    expect(result).not.toBeNull();
    expect(result!.sticky).toBe(true);
    expect(result!.retryAttempts).toBe(5);
    expect(result!.retryInterval).toBe('200ms');
    expect(result!.circuitBreaker).toBe(true);
    expect(result!.healthCheckPath).toBe('/api/health');
    expect(result!.healthCheckInterval).toBe('15s');
    expect(result!.localityAware).toBe(true);
  });

  it('should use defaults for optional labels when not set', () => {
    const labels = {
      'federation.enable': 'true',
      'federation.host': 'app.local',
      'federation.port': '3000',
    };
    const result = parser.parse(labels);
    expect(result).not.toBeNull();
    expect(result!.sticky).toBe(false);
    expect(result!.retryAttempts).toBe(3);
    expect(result!.retryInterval).toBe('100ms');
    expect(result!.circuitBreaker).toBe(false);
    expect(result!.healthCheckPath).toBe('/');
    expect(result!.healthCheckInterval).toBe('10s');
    expect(result!.localityAware).toBe(false);
  });

  it('should return null when host is missing', () => {
    const labels = {
      'federation.enable': 'true',
      'federation.port': '3000',
    };
    expect(parser.parse(labels)).toBeNull();
  });

  it('should return null when port is missing', () => {
    const labels = {
      'federation.enable': 'true',
      'federation.host': 'app.local',
    };
    expect(parser.parse(labels)).toBeNull();
  });

  it('should return null when port is invalid (non-numeric)', () => {
    const labels = {
      'federation.enable': 'true',
      'federation.host': 'app.local',
      'federation.port': 'invalid',
    };
    expect(parser.parse(labels)).toBeNull();
  });

  it('should return null when port is 0', () => {
    const labels = {
      'federation.enable': 'true',
      'federation.host': 'app.local',
      'federation.port': '0',
    };
    expect(parser.parse(labels)).toBeNull();
  });

  it('should return null when port is 65536 (out of range)', () => {
    const labels = {
      'federation.enable': 'true',
      'federation.host': 'app.local',
      'federation.port': '65536',
    };
    expect(parser.parse(labels)).toBeNull();
  });

  it('should handle port at boundary values', () => {
    const labels1 = {
      'federation.enable': 'true',
      'federation.host': 'app.local',
      'federation.port': '1',
    };
    expect(parser.parse(labels1)!.port).toBe(1);

    const labels2 = {
      'federation.enable': 'true',
      'federation.host': 'app.local',
      'federation.port': '65535',
    };
    expect(parser.parse(labels2)!.port).toBe(65535);
  });

  it('isFederationEnabled should work correctly', () => {
    expect(parser.isFederationEnabled({ 'federation.enable': 'true' })).toBe(true);
    expect(parser.isFederationEnabled({ 'federation.enable': 'false' })).toBe(false);
    expect(parser.isFederationEnabled({})).toBe(false);
  });
});
