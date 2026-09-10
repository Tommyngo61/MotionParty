import { Card, TopBar } from "@/components/Shell";

/**
 * Views whose backing services are later milestones. Saying so plainly beats
 * a screen of fabricated rows that implies the feature exists.
 */
export function Placeholder({ title, milestone, what }: { title: string; milestone: string; what: string }) {
  return (
    <>
      <TopBar title={title} subtitle={milestone} />
      <Card className="py-14 text-center">
        <p className="mx-auto max-w-md text-xs leading-relaxed text-[var(--text-lo)]">{what}</p>
      </Card>
    </>
  );
}
