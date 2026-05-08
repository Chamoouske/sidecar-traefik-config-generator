/**
 * Barrel export para todas as interfaces do core do sidecar.
 */

export type { IDockerClient } from './IDockerClient.js';
export type { ISwarmDiscovery } from './ISwarmDiscovery.js';
export type { ILabelParser } from './ILabelParser.js';
export type { IConfig } from './IConfig.js';
export type { ILogger } from './ILogger.js';
export type { IFileWriter } from './IFileWriter.js';
export type { IFederationStrategy } from './IFederationStrategy.js';
export type { IMiddlewareGenerator } from './IMiddlewareGenerator.js';
export type { ILocalConfigGenerator } from './ILocalConfigGenerator.js';
export type { IServiceLocality } from './IServiceLocality.js';
export type { IEventEmitter, EventHandler } from './IEventEmitter.js';
