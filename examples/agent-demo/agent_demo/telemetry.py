import logging
from contextlib import contextmanager
from urllib.parse import urlsplit

from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.grpc._log_exporter import OTLPLogExporter
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk._logs import LoggerProvider, LoggingHandler
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.trace import Status, StatusCode

CONVENTION = "genai@e6658f14c9d9ea0760825f029cb82e7673f48fab"


class Telemetry:
    def __init__(self, endpoint="http://127.0.0.1:4317", provider=None, log_provider=None):
        url = urlsplit(endpoint)
        if (
            url.scheme != "http"
            or not url.hostname
            or not url.port
            or url.username
            or url.password
            or url.query
            or url.fragment
            or url.path not in {"", "/"}
        ):
            raise ValueError("invalid local OTLP/gRPC endpoint")
        resource = Resource.create(
            {
                "service.name": "support-agent",
                "service.version": "0.7.0",
                "deployment.environment.name": "local",
            }
        )
        self.provider = provider or TracerProvider(resource=resource)
        if provider is None:
            self.provider.add_span_processor(
                BatchSpanProcessor(
                    OTLPSpanExporter(endpoint=endpoint, insecure=True, timeout=3),
                    max_queue_size=512,
                    max_export_batch_size=128,
                    schedule_delay_millis=1000,
                    export_timeout_millis=3000,
                )
            )
        self.tracer = self.provider.get_tracer("traceforge.agent", "0.7.0")
        self.logs = log_provider
        self.logger = logging.Logger("traceforge.agent", level=logging.INFO)
        if provider is None or log_provider is not None:
            self.logs = log_provider or LoggerProvider(resource=resource)
            if log_provider is None:
                self.logs.add_log_record_processor(
                    BatchLogRecordProcessor(
                        OTLPLogExporter(endpoint=endpoint, insecure=True, timeout=3),
                        max_queue_size=512,
                        max_export_batch_size=128,
                        schedule_delay_millis=1000,
                        export_timeout_millis=3000,
                    )
                )
            self.logger.addHandler(LoggingHandler(level=logging.INFO, logger_provider=self.logs))
        else:
            self.logger.addHandler(logging.NullHandler())

    @contextmanager
    def span(self, name, attributes, kind=trace.SpanKind.INTERNAL, context=None):
        with self.tracer.start_as_current_span(
            name,
            attributes=attributes,
            kind=kind,
            context=context,
            record_exception=False,
            set_status_on_exception=False,
        ) as span:
            yield span

    @staticmethod
    def fail(span, kind):
        # Never record arbitrary exception messages, provider bodies or inputs.
        span.set_status(Status(StatusCode.ERROR, kind))
        span.set_attribute("error.type", kind)
        span.set_attribute("traceforge.outcome", kind)
        span.add_event(
            "exception", {"exception.type": kind, "exception.message": "operation failed"}
        )

    def event(self, message, run_id, kind=None):
        attributes = {"traceforge.run.id": run_id}
        if kind:
            attributes["error.type"] = kind
        self.logger.log(logging.WARNING if kind else logging.INFO, message, extra=attributes)

    def shutdown(self):
        self.provider.force_flush(timeout_millis=3000)
        self.provider.shutdown()
        if self.logs:
            self.logs.force_flush(timeout_millis=3000)
            self.logs.shutdown()
