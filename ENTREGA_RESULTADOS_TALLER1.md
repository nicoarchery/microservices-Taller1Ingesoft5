# Entrega de Resultados - Taller 1 (DevOps + Cloud)

Este documento consolida la evidencia de los elementos desarrollados para el taller.

---

## 1) 2.5% Estrategia de branching para desarrolladores

**Estrategia usada:** GitHub Flow adaptado con rama de integración.

- Ramas principales: `main`, `develop`
- Ramas de trabajo: `feature/*`, `fix/*`, `hotfix/*`
- Flujo: `feature/fix -> develop -> main`
- Convención de commits: `feat|fix|docs|test|refactor|hotfix|chore`

**Evidencia:**
- `BRANCHING_DEV.md`
- `microservices-Taller1Ingesoft5/.github/workflows/pipeline-dev.yml`

---

## 2) 2.5% Estrategia de branching para operaciones

**Estrategia usada:** Environment Branching.

- Ramas por promoción: `develop` (dev), `staging`, `main` (prod)
- Cambios de infraestructura pasan por PR y promoción entre ambientes
- Validación previa local y luego CI de infraestructura

**Evidencia:**
- `BRANCHING_OPS.md`
- `infra-microservices-Taller1Ingesoft5/.github/workflows/pipeline-infra.yml`

---

## 3) 15.0% Patrones de diseño de nube (mínimo dos)

### Patrón 1: Competing Consumers

Se implementa en `worker` consumiendo eventos de Kafka como grupo de consumidores.
Permite escalar horizontalmente el procesamiento de votos.

**Evidencia:**
- `microservices-Taller1Ingesoft5/IMPLEMENTACION_COMPETING_CONSUMER_TALLER.md`
- `microservices-Taller1Ingesoft5/worker/main.go`

### Patrón 2: Circuit Breaker

Se implementa entre `worker` y PostgreSQL para degradar de forma controlada ante fallos de DB.
Evita cascadas de error y mejora resiliencia.

**Evidencia:**
- `microservices-Taller1Ingesoft5/IMPLEMENTACION_CIRCUIT_BREAKER_TALLER.md`
- `microservices-Taller1Ingesoft5/worker/internal/db/circuit_breaker.go`

---

## 4) 15.0% Diagrama de arquitectura

![alt text](microservices-Taller1Ingesoft5/Arquitecture.png)

**Descripción breve:**
- `vote` publica votos en Kafka.
- `worker` consume y persiste en PostgreSQL.
- `result` consulta PostgreSQL y muestra resultados.
- Terraform define la infraestructura y GitHub Actions automatiza CI.

---

## 5) 15.0% Pipelines de desarrollo (incluidos scripts)

**Pipeline implementado:** CI de microservicios en GitHub Actions.

Etapas implementadas:
1. Detección de cambios por servicio
2. Lint por stack (Java/Go/Node)
3. Tests por stack
4. Build de imágenes Docker
5. Notificación de resultado

**Scripts de soporte:**
- `scripts/deploy-manual.sh`
- `scripts/health-check.sh`

**Evidencia:**
- `microservices-Taller1Ingesoft5/.github/workflows/pipeline-dev.yml`
- `scripts/deploy-manual.sh`
- `scripts/health-check.sh`

---

## 6) 5.0% Pipelines de infraestructura (incluidos scripts)

**Pipeline implementado:** Validación CI de Terraform.

Etapas implementadas:
1. `terraform fmt -check -recursive`
2. `terraform init -backend=false`
3. `terraform validate`
4. Notificación final

**Scripts de soporte:**
- `scripts/setup-terraform-backend.sh`
- `scripts/setup-github-secrets.sh`

**Evidencia:**
- `infra-microservices-Taller1Ingesoft5/.github/workflows/pipeline-infra.yml`
- `scripts/setup-terraform-backend.sh`
- `scripts/setup-github-secrets.sh`

---

## 7) 20.0% Implementación de la infraestructura

Se implementó IaC real con Terraform y despliegue en Azure por ambientes.

### Estructura de infraestructura

- Módulos:
  - `modules/resource-group`
  - `modules/networking`
  - `modules/acr`
  - `modules/storage`
  - `modules/aks`
  - `modules/platform`
- Ambientes:
  - `environments/dev`
  - `environments/staging`
  - `environments/prod`

### Recursos implementados

- Resource Group
- VNet + subnets + NSG
- AKS
- ACR
- Storage Account (incluyendo backend de estado)
- Log Analytics

### Evidencia operativa

- `terraform fmt` y `terraform validate` ejecutados con éxito por ambiente.
- `terraform plan/apply` ejecutado en `dev`.
- AKS accesible con `kubectl get nodes` (nodo en estado `Ready`).

**Evidencia:**
- `infra-microservices-Taller1Ingesoft5/modules/**`
- `infra-microservices-Taller1Ingesoft5/environments/**`
- `infra-microservices-Taller1Ingesoft5/AZURE_RUNBOOK.md`

---

## 8) 15.0% Demostración en vivo de cambios en el pipeline

### Guion recomendado de demo (5-8 minutos)

1. Crear rama de demo:
   - `feature/demo-pipeline`
2. Hacer un cambio mínimo en un archivo del repo correspondiente.
3. Ejecutar:
   - `git add .`
   - `git commit -m "docs: demo pipeline"`
   - `git push origin feature/demo-pipeline`
4. Abrir GitHub Actions y mostrar:
   - disparo automático del workflow
   - ejecución de jobs
   - resultado final
5. Repetir en repo de infraestructura con un cambio mínimo (`README` o variable no crítica).

### Qué mostrar al profesor

- Trigger por rama según estrategia de branching.
- Evidencia de pasos de CI (lint/test/build o validate).
- Trazabilidad commit -> workflow run -> resultado.

---

## 9) 10.0% Entrega de resultados (documentación completa)

Esta entrega incluye documentación para todos los componentes desarrollados:

- Estrategia Dev: `BRANCHING_DEV.md`
- Estrategia Ops: `BRANCHING_OPS.md`
- Patrones cloud: 
  - `microservices-Taller1Ingesoft5/IMPLEMENTACION_COMPETING_CONSUMER_TALLER.md`
  - `microservices-Taller1Ingesoft5/IMPLEMENTACION_CIRCUIT_BREAKER_TALLER.md`
- Pipelines:
  - `microservices-Taller1Ingesoft5/.github/workflows/pipeline-dev.yml`
  - `infra-microservices-Taller1Ingesoft5/.github/workflows/pipeline-infra.yml`
- Infraestructura IaC:
  - `infra-microservices-Taller1Ingesoft5/modules/**`
  - `infra-microservices-Taller1Ingesoft5/environments/**`
- Runbook operativo:
  - `infra-microservices-Taller1Ingesoft5/AZURE_RUNBOOK.md`
- Guía de organización del taller:
  - `GUÍA_TALLER_PIPELINES_INFRA.md`

---

## Estado final del taller

- ✅ Estrategias de branching Dev/Ops definidas
- ✅ 2 patrones cloud implementados
- ✅ Diagrama de arquitectura entregado
- ✅ Pipeline Dev funcional
- ✅ Pipeline Infra funcional
- ✅ Infraestructura implementada con Terraform
- ✅ Guion de demo en vivo definido
- ✅ Documentación consolidada de entrega
