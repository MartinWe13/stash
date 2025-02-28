import React from "react";
import { useChangelogStorage } from "src/hooks";
import Version from "./Version";
import V010 from "./versions/v010.md";
import V011 from "./versions/v011.md";
import V020 from "./versions/v020.md";
import V021 from "./versions/v021.md";
import V030 from "./versions/v030.md";
import V040 from "./versions/v040.md";
import V050 from "./versions/v050.md";
import V060 from "./versions/v060.md";
import V070 from "./versions/v070.md";
import V080 from "./versions/v080.md";
import V090 from "./versions/v090.md";
import V0100 from "./versions/v0100.md";
import { MarkdownPage } from "../Shared/MarkdownPage";

// to avoid use of explicit any
type Module = typeof V010;

const Changelog: React.FC = () => {
  const [{ data, loading }, setOpenState] = useChangelogStorage();

  const stashVersion = process.env.REACT_APP_STASH_VERSION;
  const buildTime = process.env.REACT_APP_DATE;

  let buildDate;
  if (buildTime) {
    buildDate = buildTime.substring(0, buildTime.indexOf(" "));
  }

  if (loading) return <></>;

  const openState = data?.versions ?? {};

  const setVersionOpenState = (key: string, state: boolean) =>
    setOpenState({
      versions: {
        ...openState,
        [key]: state,
      },
    });

  interface IStashRelease {
    version: string;
    date?: string;
    page: Module;
    defaultOpen?: boolean;
  }

  // after new release:
  // add entry to releases, using the current* fields
  // then update the current fields.
  const currentVersion = stashVersion || "v0.10.0";
  const currentDate = buildDate;
  const currentPage = V0100;

  const releases: IStashRelease[] = [
    {
      version: currentVersion,
      date: currentDate,
      page: currentPage,
      defaultOpen: true,
    },
    {
      version: "v0.9.0",
      date: "2021-09-06",
      page: V090,
    },
    {
      version: "v0.8.0",
      date: "2021-07-02",
      page: V080,
    },
    {
      version: "v0.7.0",
      date: "2021-05-15",
      page: V070,
    },
    {
      version: "v0.6.0",
      date: "2021-03-29",
      page: V060,
    },
    {
      version: "v0.5.0",
      date: "2021-02-23",
      page: V050,
    },
    {
      version: "v0.4.0",
      date: "2020-11-24",
      page: V040,
    },
    {
      version: "v0.3.0",
      date: "2020-09-02",
      page: V030,
    },
    {
      version: "v0.2.1",
      date: "2020-06-10",
      page: V021,
    },
    {
      version: "v0.2.0",
      date: "2020-06-06",
      page: V020,
    },
    {
      version: "v0.1.1",
      date: "2020-02-25",
      page: V011,
    },
    {
      version: "v0.1.0",
      date: "2020-02-24",
      page: V010,
    },
  ];

  return (
    <>
      <h1 className="mb-4">Changelog:</h1>
      {releases.map((r) => (
        <Version
          key={r.version}
          version={r.version}
          date={r.date}
          openState={openState}
          setOpenState={setVersionOpenState}
          defaultOpen={r.defaultOpen}
        >
          <MarkdownPage page={r.page} />
        </Version>
      ))}
    </>
  );
};

export default Changelog;
