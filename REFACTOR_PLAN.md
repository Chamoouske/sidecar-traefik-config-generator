# Plano de Refatoração: Suporte a GENERATION_MODE

## Contexto da Refatoração

O objetivo é formalizar e otimizar o suporte aos modos de geração `all`, `global` e `local` controlados pela variável de ambiente `GENERATION_MODE`, refatorando a estrutura de orquestração existente e removendo componentes obsoletos.

- **Modo `global`**: Executa APENAS em um nó `manager` do Docker Swarm. Usa as APIs do Swarm para visibilidade de cluster. Gera APENAS arquivos compartilhados (`/shared/federation/*`, `/shared/middlewares/*`).
- **Modo `local`**: Executa em QUALQUER nó (manager ou worker). Inspeciona APENAS o daemon local (não o estado completo do Swarm). Gera APENAS arquivos locais (`/local/generated/*`).
- **Modo `all`**: Gera AMBOS os tipos de arquivos (comportamento atual). Útil para desenvolvimento/testes.

## Pontos-chave do Plano

### 1. Atualização de Configuração e Tipos

- **Status Atual**: Os tipos [`GenerationMode`](src/types/config.ts:5) e a propriedade [`mode`](src/types/config.ts:8) na interface [`AppConfig`](src/types/config.ts:7) já estão definidos em [`src/types/config.ts`](src/types/config.ts). A leitura e validação da variável de ambiente `GENERATION_MODE` são realizadas pela função [`parseGenerationMode()`](src/config/index.ts:13) em [`src/config/index.ts`](src/config/index.ts), que já trata os valores `all`, `global` e `local`.
- **Ações Necessárias**: Nenhuma alteração significativa é necessária nesta seção, pois a infraestrutura de configuração já suporta `GENERATION_MODE`.

### 2. Refatoração dos Serviços de Descoberta

- **Status Atual**:
    - [`LocalDiscoveryService`](src/services/LocalDiscoveryService.ts) já existe e utiliza `dockerClient.listContainers()` para descobrir serviços no nó local, filtrando por labels. Isso atende ao requisito do modo `local`.
    - [`SwarmDiscoveryService`](src/services/SwarmDiscoveryService.ts) já existe e utiliza `dockerClient.getServices()` e `dockerClient.getServiceTasks()` para descobrir todos os serviços federados no Swarm, adequado para o modo `global`.
    - **Redundância**: O `SwarmDiscoveryService` atualmente também implementa `ILocalDiscoveryService` e possui um método `discoverLocalServices()`, que é redundante com o [`LocalDiscoveryService`](src/services/LocalDiscoveryService.ts) dedicado.
- **Ações Necessárias**:
    - Remover a implementação de `ILocalDiscoveryService` e o método `discoverLocalServices()` de [`src/services/SwarmDiscoveryService.ts`](src/services/SwarmDiscoveryService.ts), pois a responsabilidade de descoberta local será exclusiva do [`LocalDiscoveryService`](src/services/LocalDiscoveryService.ts).
    - Garantir que o `SwarmDiscoveryService` seja injetado apenas quando o modo `global` estiver ativo, e o `LocalDiscoveryService` quando o modo `local` estiver ativo. (Isso já é feito em [`src/index.ts`](src/index.ts)).

### 3. Nova Estrutura de Orquestração

- **Status Atual**:
    - [`GlobalOrchestratorService`](src/orchestration/GlobalOrchestratorService.ts) já existe, é responsável pela geração de configurações compartilhadas (federação e middlewares) e utiliza `ISwarmDiscovery`.
    - [`LocalOrchestratorService`](src/orchestration/LocalOrchestratorService.ts) já existe, é responsável pela geração de configurações locais (específicas do nó) e utiliza `ILocalDiscoveryService`.
    - [`ModeOrchestratorService`](src/orchestration/ModeOrchestratorService.ts) já existe e atua como fachada, instanciando e executando os orquestradores `Global` e/ou `Local` com base no `GENERATION_MODE`.
    - O [`ConfigOrchestratorService`](src/services/ConfigOrchestratorService.ts) existe e centraliza a lógica de geração, sendo o componente a ser removido.
- **Ações Necessárias**:
    - **Remover** o arquivo [`src/services/ConfigOrchestratorService.ts`](src/services/ConfigOrchestratorService.ts) e todas as suas referências no projeto.
    - Confirmar que a lógica de `cleanupStaleFiles` e `generateForService` do `ConfigOrchestratorService` está adequadamente distribuída e duplicada (quando relevante) no `GlobalOrchestratorService` e `LocalOrchestratorService`. Pelo que foi analisado, ambos já possuem sua própria lógica de limpeza.

### 4. Ajustes no Bootstrap (`src/index.ts`)

- **Status Atual**: O arquivo [`src/index.ts`](src/index.ts) já implementa a lógica condicional para instanciar `GlobalOrchestratorService` e `LocalOrchestratorService` com base em `GENERATION_MODE` (`all`, `global`, `local`). O [`ModeOrchestratorService`](src/orchestration/ModeOrchestratorService.ts) é o ponto de entrada principal para os ciclos de geração. A função auxiliar [`ensureDirectories()`](src/index.ts:190) já garante a criação dos diretórios de saída relevantes para cada modo.
- **Ações Necessárias**: 
    - Nenhuma alteração significativa é necessária, além de remover as importações e instâncias do `ConfigOrchestratorService` caso ainda existam. A estrutura atual já está bem definida.

### 5. Estratégia de Testes

- **Status Atual**: Existem arquivos de teste para a maioria dos serviços, incluindo [`src/__tests__/LocalDiscoveryService.test.ts`](src/__tests__/LocalDiscoveryService.test.ts), [`src/__tests__/SwarmDiscoveryService.test.ts`](src/__tests__/SwarmDiscoveryService.test.ts), [`src/__tests__/GlobalOrchestrator.test.ts`](src/__tests__/GlobalOrchestrator.test.ts), [`src/__tests__/LocalOrchestrator.test.ts`](src/__tests__/LocalOrchestrator.test.ts), e [`src/__tests__/ModeOrchestrator.test.ts`](src/__tests__/ModeOrchestrator.test.ts).
- **Ações Necessárias**:
    - **Atualizar Testes Existentes**:
        - `SwarmDiscoveryService.test.ts`: Remover testes relacionados à descoberta local, se houver.
        - `ConfigOrchestratorService.test.ts`: Este arquivo de teste deve ser removido junto com o serviço.
    - **Novos Testes**:
        - Garantir cobertura completa para os cenários de `ModeOrchestratorService`, verificando que ele chama os orquestradores corretos para cada `GENERATION_MODE`.
        - Adicionar testes de integração que simulem o ambiente Docker para cada modo (`global`, `local`, `all`) e verifiquem a geração correta dos arquivos de configuração nos diretórios esperados.

### 6. Gerenciamento de Pastas

- **Status Atual**: O [`FileWriterService`](src/filesystem/FileWriterService.ts) já oferece métodos robustos como `writeYaml()`, `deleteFile()`, `ensureDirectory()`, e `listFiles()`. A função auxiliar [`ensureDirectories()`](src/index.ts:190) em [`src/index.ts`](src/index.ts) já é mode-aware e cria as pastas `/shared/federation`, `/shared/middlewares` (para `global`/`all`) e `/local/generated` (para `local`/`all`) conforme a necessidade.
- **Ações Necessárias**: Nenhuma alteração na lógica de gerenciamento de pastas é necessária, pois a implementação existente já é adequada.

### 7. Loop Prevention e Locality-Aware Routing

- **Status Atual**: Não há informações explícitas sobre como essas regras são aplicadas no código atual, mas são mencionadas como "regras existentes". O [`ServiceLocalityService`](src/services/ServiceLocalityService.ts) e [`LabelParserService`](src/services/LabelParserService.ts) são prováveis candidatos para conter essa lógica.
- **Ações Necessárias**:
    - **Verificar Implementação**: Inspecionar [`src/services/ServiceLocalityService.ts`](src/services/ServiceLocalityService.ts) e [`src/services/LabelParserService.ts`](src/services/LabelParserService.ts) para entender como "Loop Prevention" e "Locality-Aware Routing" são atualmente implementados.
    - **Garantir Aplicação em Modos**: Confirmar que a lógica é aplicada corretamente por `FederationConfigGeneratorService` e `LocalConfigGeneratorService` em seus respectivos contextos, independentemente do `GENERATION_MODE`. Não deve ser necessário duplicar essa lógica, mas sim garantir que os geradores a utilizem.

## Plano de Implementação Passo a Passo

Este plano de implementação é ordenado para minimizar interrupções e garantir um fluxo de trabalho lógico.

1.  **Refatorar `SwarmDiscoveryService`**:
    *   [ ] Remover a implementação de `ILocalDiscoveryService` e o método `discoverLocalServices()` de [`src/services/SwarmDiscoveryService.ts`](src/services/SwarmDiscoveryService.ts).
    *   [ ] Atualizar testes em [`src/__tests__/SwarmDiscoveryService.test.ts`](src/__tests__/SwarmDiscoveryService.test.ts) para refletir a remoção da funcionalidade de descoberta local.

2.  **Remover `ConfigOrchestratorService`**:
    *   [ ] Remover o arquivo [`src/services/ConfigOrchestratorService.ts`](src/services/ConfigOrchestratorService.ts).
    *   [ ] Remover o arquivo de teste [`src/__tests__/ConfigOrchestratorService.test.ts`](src/__tests__/ConfigOrchestratorService.test.ts) (se existir).
    *   [ ] Remover todas as referências (`import` e instâncias) a `ConfigOrchestratorService` em outros arquivos, especialmente em [`src/index.ts`](src/index.ts).

3.  **Verificar e Otimizar `ServiceLocalityService` e `LabelParserService`**:
    *   [ ] Ler [`src/services/ServiceLocalityService.ts`](src/services/ServiceLocalityService.ts) para entender a lógica de "Locality-Aware Routing".
    *   [ ] Ler [`src/services/LabelParserService.ts`](src/services/LabelParserService.ts) para entender a lógica de "Loop Prevention" (se aplicável, ou outras regras de labels).
    *   [ ] Confirmar que `FederationConfigGeneratorService` e `LocalConfigGeneratorService` utilizam esses serviços adequadamente.

4.  **Aprimorar Cobertura de Testes para Orquestradores**:
    *   [ ] Adicionar ou expandir testes de integração para [`src/orchestration/ModeOrchestratorService.ts`](src/orchestration/ModeOrchestratorService.ts) para cobrir todos os cenários de `GENERATION_MODE`.
    *   [ ] Adicionar testes de ponta a ponta (E2E) que simulem um ambiente Docker (swarm e contêineres locais) e verifiquem a criação correta dos arquivos de configuração para cada `GENERATION_MODE` (`all`, `global`, `local`).

## Diagrama de Arquitetura de Orquestração (Mermaid)

```mermaid
graph TD
    A[src/index.ts - Bootstrap] --> B{Configuração: GENERATION_MODE};

    B -- GENERATION_MODE = 'all' --> C1[Instancia GlobalOrchestratorService];
    B -- GENERATION_MODE = 'all' --> C2[Instancia LocalOrchestratorService];
    B -- GENERATION_MODE = 'global' --> C1;
    B -- GENERATION_MODE = 'local' --> C2;

    C1 --> D1[SwarmDiscoveryService];
    C1 --> D2[FederationConfigGeneratorService];
    C1 --> D3[MiddlewareConfigGeneratorService];
    C1 --> D4[FileWriterService];

    C2 --> E1[LocalDiscoveryService];
    C2 --> E2[LocalConfigGeneratorService];
    C2 --> E3[FileWriterService];

    F[ModeOrchestratorService] -- Orquestra --> C1;
    F -- Orquestra --> C2;

    G[DockerClientService] -- Usado por --> D1;
    G -- Usado por --> E1;

    H[ServiceLocalityService] -- Usado por --> D2;
    H -- Usado por --> E2;

    I[LabelParserService] -- Usado por --> D1;
    I -- Usado por --> E1;
    I -- Usado por --> D2;
    I -- Usado por --> D3;
    I -- Usado por --> E2;

    J[LoggerService] -- Usado por todos os serviços relevantes --> K[Logs];

    D1 -- API Swarm --> L[Docker Swarm API];
    E1 -- API Local Docker --> M[Docker Daemon Local];

    D2 -- Gera --> N[shared/federation/*];
    D3 -- Gera --> O[shared/middlewares/*];
    E2 -- Gera --> P[local/generated/*];

    N --> Q[Arquivos de Configuração Traefik];
    O --> Q;
    P --> Q;
