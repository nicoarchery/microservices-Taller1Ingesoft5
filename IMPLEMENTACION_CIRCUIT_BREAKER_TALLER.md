# Implementación de Resiliencia (Circuit Breaker) en el Worker

## 1) Objetivo

Fortalecer el microservicio `worker` para que, ante fallos de PostgreSQL, no colapse ni haga reintentos agresivos. Se implementó el patrón **Circuit Breaker** junto con **backoff exponencial + jitter** y configuración por entorno para despliegues en Kubernetes/Okteto.

---

## 2) Cambios implementados (detalle técnico)

### 2.1 Refactor de responsabilidades del worker

Se separó la lógica para que `main.go` quede como orquestador:

- Inicializa conexiones (DB/Kafka)
- Arranca consumo de mensajes
- Delega persistencia a una capa de acceso a datos

Archivos involucrados:

- `worker/main.go`
- `worker/internal/db/store.go`

### 2.2 Circuit Breaker para PostgreSQL

Se creó un breaker dedicado a la escritura en base de datos:

- `worker/internal/db/circuit_breaker.go`

Parámetros implementados:

- `Name`: `PostgreSQL-CB`
- `MaxRequests`: `1` (half-open conservador)
- `Interval`: `60s`
- `Timeout`: `30s`
- `ReadyToTrip`: abre circuito con al menos 5 requests y ratio de fallos >= 50%
- `OnStateChange`: logging de transición de estados

### 2.3 Persistencia protegida por breaker

Se encapsuló la escritura a Postgres en `Store.SaveVote(...)`:

- `worker/internal/db/store.go`

Comportamiento:

- Si DB responde: persiste y retorna
- Si breaker está abierto: retorna `ErrCircuitOpen`
- Si hay otro error: retorna error envuelto con contexto

### 2.4 Estrategia de reintentos en runtime

En `main.go` se centralizó en `persistVote(...)`:

- Retry infinito del **mismo mensaje** hasta persistir (evita descartarlo)
- Manejo explícito de `ErrCircuitOpen`
- Manejo de errores generales de DB

### 2.5 Backoff exponencial con tope

Se implementó crecimiento de espera:

- Inicial: `1s`
- Duplicación por intento
- Tope: `30s`

Función:

- `nextRetryDelay(...)` en `worker/main.go`

### 2.6 Jitter para evitar sincronización entre réplicas

Se agregó variación aleatoria controlada en los delays:

- Factor por defecto: `0.2` (±20%)
- Función: `jitterDelay(...)`
- Clamp de seguridad: rango `[0, 1]`

Esto reduce picos simultáneos de reintentos (thundering herd) cuando hay múltiples réplicas del worker.

### 2.7 Configuración por variables de entorno

En `main.go` se agregó lectura robusta de entorno con fallback:

- `DB_RETRY_INITIAL` (duración, ej. `1s`)
- `DB_RETRY_MAX` (duración, ej. `30s`)
- `DB_RETRY_JITTER` (float, ej. `0.2`)

Si el valor es inválido:

- se registra en log
- se usa el valor por defecto

Funciones:

- `durationFromEnv(...)`
- `floatFromEnv(...)`

### 2.8 Dependencia y ejecución del módulo

Se agregó dependencia de gobreaker y ajuste de ejecución:

- `worker/go.mod`: `github.com/sony/gobreaker/v2`
- `worker/Makefile`: `go run .` (para compilar paquetes internos correctamente)

---

## 3) Cambios en YAML (Helm/Kubernetes)

### 3.1 ¿Qué son estos YAML?

Los YAML bajo `worker/chart/...` son **manifiestos Helm** que generan recursos de Kubernetes:

- `templates/deployment.yaml`: define el `Deployment` (pod(s), contenedor, variables de entorno, etc.)
- `values.yaml`: define parámetros configurables del chart (valores por defecto y overrides por ambiente)

### 3.2 Qué se cambió

#### `worker/chart/templates/deployment.yaml`

Se añadieron variables de entorno al contenedor `worker`:

- `DB_RETRY_INITIAL`
- `DB_RETRY_MAX`
- `DB_RETRY_JITTER`

Tomadas desde `.Values.resilience.dbRetry.*`.

#### `worker/chart/values.yaml`

Se añadió sección de resiliencia con defaults:

```yaml
resilience:
  dbRetry:
    initial: "1s"
    max: "30s"
    jitter: "0.2"
```

Con esto, operaciones puede ajustar resiliencia por ambiente sin tocar código.

---

## 4) Relación con la rúbrica del taller

A continuación, qué puntos impacta directamente esta implementación:

### 3) Patrones de diseño de nube (15%)

Impacto: **alto**

- Se implementó explícitamente **Circuit Breaker**
- Se complementó con **Retry with Exponential Backoff + Jitter** (patrón de resiliencia operacional)

> Ya tienes mínimo dos patrones cloud de resiliencia para sustentar en la entrega.

### 5) Pipelines de desarrollo (15%)

Impacto: **medio-alto**

- Al estar parametrizado por entorno, el pipeline de app puede desplegar con distintos valores sin recompilar
- Facilita promoción dev/qa/prod con configuración por variables

### 6) Pipelines de infraestructura (5%)

Impacto: **alto**

- El comportamiento resiliente se controla por Helm values
- Se integra naturalmente en pipeline de despliegue (`helm upgrade --set ...`)

### 7) Implementación de infraestructura (20%)

Impacto: **alto**

- Quedó reflejado en `Deployment` y `values` del chart del worker
- Cambios reales en artefactos de infraestructura (K8s + Helm)

### 8) Demostración en vivo de cambios en pipeline (15%)

Impacto: **alto**

- Puedes demostrar en vivo cómo cambia el comportamiento del worker al modificar `DB_RETRY_*` desde pipeline/Helm

### 9) Entrega de resultados y documentación (10%)

Impacto: **alto**

- Este documento cubre evidencia técnica, motivación, cambios por archivo y trazabilidad con rúbrica

---

## 5) Evidencia por archivo (resumen rápido)

- `worker/internal/db/circuit_breaker.go`: implementación del patrón Circuit Breaker
- `worker/internal/db/store.go`: persistencia protegida por breaker
- `worker/main.go`: manejo operativo de errores + retries + backoff + jitter + env config
- `worker/go.mod`: dependencia `gobreaker`
- `worker/Makefile`: ajuste de comando de ejecución
- `worker/chart/templates/deployment.yaml`: inyección de variables de resiliencia al contenedor
- `worker/chart/values.yaml`: valores configurables por entorno

---

## 6) Cómo usar la configuración en despliegue

Ejemplos:

- `DB_RETRY_INITIAL=2s`
- `DB_RETRY_MAX=45s`
- `DB_RETRY_JITTER=0.3`

En Helm se pueden setear por `values.yaml` o por parámetros `--set` en pipeline.

---

## 7) Notas importantes para sustentación

1. Esta implementación prioriza **disponibilidad del proceso** (el worker no cae por fallos transitorios de DB).
2. Se evita retry agresivo que pueda empeorar incidentes.
3. La configuración por entorno habilita operación ágil por equipo DevOps.
4. Para evolución futura, se recomienda agregar DLQ o estrategia de requeue explícita para escenarios prolongados de indisponibilidad.
