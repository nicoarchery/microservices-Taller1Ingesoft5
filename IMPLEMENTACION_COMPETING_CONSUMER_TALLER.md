# Implementación del Patrón Competing Consumer en el Worker

## 1) Objetivo

Implementar el patrón **Competing Consumer** para permitir el procesamiento paralelo de votos desde Kafka mediante múltiples instancias del microservicio `worker`, mejorando la escalabilidad horizontal y el throughput del sistema.

---

## 2) Cambios implementados (detalle técnico)

### 2.1 Refactor del consumidor Kafka

**Archivo:** `worker/main.go`

Se migró de un consumidor de partición única (`ConsumePartition`) a un **Consumer Group** (`sarama.NewConsumerGroup`).

| Antes | Después |
|-------|---------|
| `master.ConsumePartition(*topic, 0, sarama.OffsetOldest)` | `sarama.NewConsumerGroup(brokers, groupID, config)` |
| Consumer independiente | Miembro de un consumer group (`vote-workers`) |
| Lee solo partición 0 | Distribuye particiones entre workers automáticamente |

**Estrategia de rebalanceo:** `BalanceStrategyRoundRobin` - distribuye las particiones de forma equitativa entre los workers disponibles.

### 2.2 Implementación del handler de Consumer Group

Se creó la estructura `VoteHandler` que implementa la interfaz `sarama.ConsumerGroupHandler`:

```go
type VoteHandler struct {
    store      *storedb.Store
    msgCounter *int
}
```

Métodos implementados:
- `Setup()`: Inicialización de la sesión del consumer
- `Cleanup()`: Limpieza al finalizar sesión
- `ConsumeClaim()`: Procesamiento de mensajes con confirmación de offsets (`MarkMessage`)

### 2.3 Escalado horizontal en Kubernetes

**Archivo:** `worker/chart/values.yaml`

```yaml
replicaCount: 3  # Anteriormente: 1
```

Esto despliega 3 instancias del worker que compiten por procesar mensajes de Kafka.

---

## 3) Justificación de la implementación

### 3.1 ¿Por qué Competing Consumer?

| Problema sin el patrón | Solución con el patrón |
|------------------------|------------------------|
| Un solo worker procesa todos los mensajes (cuello de botella) | Múltiples workers procesan en paralelo desde diferentes particiones |
| Si el worker falla, no hay procesamiento | Si un worker falla, los otros continúan automáticamente |
| No se aprovecha el particionamiento de Kafka | Cada worker consume de particiones asignadas por el coordinador |

### 3.2 Complementariedad con Circuit Breaker

La combinación de ambos patrones crea una arquitectura resiliente y escalable:

```
┌─────────────┐     ┌──────────┐     ┌──────────────────────────────┐
│   Kafka     │     │  3       │     │  3 instancias del worker     │
│   Topic     │────▶│ Partic.  │────▶│  (Competing Consumers)       │
│  (votes)    │     │          │     │                              │
│             │     │          │     │  ┌─────┐ ┌─────┐ ┌─────┐    │
│ Partición 0 │────▶│ Part. 0  │────▶│ │ W-1 │ │ W-2 │ │ W-3 │    │
│ Partición 1 │────▶│ Part. 1  │────▶│ └─────┘ └─────┘ └─────┘    │
│ Partición 2 │────▶│ Part. 2  │────▶│                              │
└─────────────┘     └──────────┘     └──────────┬───────────────────┘
                                               │
                                               ▼
                                        [Circuit Breaker]
                                               │
                                               ▼
                                         PostgreSQL
```

| Patrón | Función en el sistema |
|--------|----------------------|
| **Competing Consumer** | Distribuye carga entre workers, permite escalabilidad horizontal |
| **Circuit Breaker** | Protege la base de datos de fallos en cascada, implementa backoff exponencial |

### 3.3 Ventajas específicas obtenidas

1. **Throughput aumentado**: 3 workers pueden procesar 3x más mensajes por segundo que uno solo
2. **Alta disponibilidad**: Si un pod falla en Kubernetes, los otros 2 continúan operando
3. **Rebalanceo automático**: Kafka reasigna particiones cuando se añaden/quitan workers
4. **Tolerancia a fallos**: Un worker con problemas de DB (CB abierto) no afecta a los demás

---

## 4) Qué mejoró respecto a la versión anterior

| Aspecto | Versión anterior (Circuit Breaker solo) | Versión actual (CB + Competing Consumer) |
|---------|----------------------------------------|------------------------------------------|
| **Escalabilidad** | Vertical (única opción: más CPU/RAM al worker) | Horizontal (añadir más réplicas en Kubernetes) |
| **Throughput máximo** | Limitado por velocidad de un solo proceso | Suma del throughput de todos los workers |
| **Disponibilidad** | Si el worker cae, todo el procesamiento se detiene | Si un worker cae, el consumer group reasigna particiones |
| **Recuperación** | Reinicio manual requerido | Auto-reconexión y rebalanceo automático |
| **Observabilidad** | Logs de un solo worker | Logs de múltiples workers identificados por pod |

**Logs mejorados:** Ahora cada mensaje muestra la partición y offset, facilitando debugging:
```
[Worker] Received message: partition=1 offset=42 key=user123 vote=tacos
```

---

## 5) Qué podría faltar o mejoras futuras

### 5.1 Configuración de particiones de Kafka

**Pendiente:** Configurar el topic `votes` en Kafka con múltiples particiones (ej: 3 o 6).

Actualmente el sistema puede tener menos particiones que workers, limitando el paralelismo. Se recomienda:
- Número de particiones >= Número de workers
- Ejemplo: 3 workers → mínimo 3 particiones

### 5.2 Offset commit automático vs manual

**Mejora potencial:** Implementar commit síncrono de offsets solo después de confirmar persistencia en PostgreSQL:

```go
// Actual: MarkMessage inmediato
session.MarkMessage(msg, "")

// Mejora: Commit solo tras éxito de DB
if err := persistWithSuccess(msg); err == nil {
    session.MarkMessage(msg, "")
}
```

Esto evitaría pérdida de mensajes si un worker se reinicia durante el procesamiento.

### 5.3 Métricas y monitoreo

**Pendiente:** Añadir métricas Prometheus para observar:
- Mensajes procesados por worker
- Latencia de procesamiento por partición
- Rebalances del consumer group
- Estados del circuit breaker por instancia

### 5.4 Graceful shutdown mejorado

**Mejora potencial:** Implementar draining de mensajes en curso antes de cerrar:

```go
// Permitir que los mensajes en proceso terminen antes del shutdown
shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
```

### 5.5 Dead Letter Queue (DLQ)

**Mejora futura:** Mensajes que fallen persistentemente (después de N reintentos) se envíen a un topic de DLQ para análisis posterior, evitando bloquear el procesamiento de nuevos mensajes.

---

## 6) Validación del funcionamiento

Para verificar que el patrón funciona correctamente:

```bash
# Ver los 3 pods del worker corriendo
kubectl get pods -l app=worker

# Ver logs de un worker específico
kubectl logs -f worker-7d9f4b8c5-x2v9n

# Ver información del consumer group en Kafka
kafka-consumer-groups.sh --bootstrap-server kafka:9092 --describe --group vote-workers
```

Salida esperada del consumer group:
```
GROUP           TOPIC   PARTITION  CURRENT-OFFSET  LOG-END-OFFSET  LAG  CONSUMER-ID
vote-workers    votes   0          150             152             2    worker-1-...
vote-workers    votes   1          200             201             1    worker-2-...
vote-workers    votes   2          180             180             0    worker-3-...
```

---

## 7) Resumen ejecutivo

Se implementó el patrón **Competing Consumer** migrando el worker de un consumidor de partición única a un **Consumer Group** con 3 réplicas en Kubernetes. Esto permite:

- ✅ Procesamiento paralelo de votos desde Kafka
- ✅ Escalabilidad horizontal (añadir/quitar workers sin downtime)
- ✅ Alta disponibilidad mediante distribución de carga
- ✅ Integración perfecta con el Circuit Breaker existente
- ✅ Auto-rebalanceo y recuperación automática ante fallos

**Resultado:** Un sistema resiliente y escalable que aprovecha al máximo la arquitectura distribuida de Kafka.
