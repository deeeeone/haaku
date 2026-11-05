import { createFileRoute, useNavigate } from '@tanstack/react-router'
import {
  usePerson,
  usePersonMovies,
  usePersonShows,
} from '@/api/hooks/usePeople'
import { Page, Section } from '@/components/layout'
import {
  Button,
  DetailStat,
  FactRow,
  Hero,
  InfoPanel,
  PosterCard,
  Rail,
  ShimmerText,
} from '@/components/ui'
import { formatDate } from '@/lib/utils'

export const Route = createFileRoute('/people/$id')({
  component: PersonDetailPage,
})

function PersonDetailPage() {
  const { id } = Route.useParams()
  const navigate = useNavigate()

  const { data: person, isLoading, isError } = usePerson(id)
  const { data: movieCredits } = usePersonMovies(id)
  const { data: showCredits } = usePersonShows(id)

  if (isLoading) {
    return (
      <Page>
        <div className="space-y-6 py-16">
          <ShimmerText className="h-[300px] w-full rounded-sf-xl" />
          <ShimmerText className="h-8 w-64" />
          <ShimmerText className="h-4 w-full" />
        </div>
      </Page>
    )
  }

  if (isError || !person) {
    return (
      <Page>
        <div className="flex flex-col items-center gap-4 py-24 text-center">
          <p className="text-lg font-semibold text-sf-ink">Person not found</p>
          <Button variant="glass" onClick={() => navigate({ to: '/search' })}>
            Search people
          </Button>
        </div>
      </Page>
    )
  }

  const movies = movieCredits?.map((c) => c.movie).filter(Boolean) ?? []
  const shows = showCredits?.map((c) => c.show).filter(Boolean) ?? []

  return (
    <Page>
      <Hero
        title={person.name}
        subtitle={
          person.biography
            ? person.biography.length > 300
              ? `${person.biography.slice(0, 300)}...`
              : person.biography
            : undefined
        }
        image={person.images?.headshot ?? person.images?.poster}
      />

      <div className="mt-6 grid grid-cols-2 gap-4 sm:grid-cols-4">
        {person.birthday && (
          <DetailStat label="Born" value={formatDate(person.birthday)} />
        )}
        {person.birthplace && (
          <DetailStat label="Birthplace" value={person.birthplace} />
        )}
        {person.death && (
          <DetailStat label="Died" value={formatDate(person.death)} />
        )}
        <DetailStat
          label="Credits"
          value={`${movies.length} movies · ${shows.length} shows`}
        />
      </div>

      {person.biography && (
        <Section title="Biography">
          <InfoPanel>
            <p className="text-sf-ink-muted leading-relaxed whitespace-pre-line">
              {person.biography}
            </p>
            <div className="mt-4 grid grid-cols-1 gap-1 sm:grid-cols-2">
              {person.birthday && (
                <FactRow label="Born" value={formatDate(person.birthday)} />
              )}
              {person.birthplace && (
                <FactRow label="Birthplace" value={person.birthplace} />
              )}
              {person.death && (
                <FactRow label="Died" value={formatDate(person.death)} />
              )}
              {person.homepage && (
                <FactRow
                  label="Homepage"
                  value={
                    <a
                      href={person.homepage}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-sf-accent-2 hover:underline"
                    >
                      Visit
                    </a>
                  }
                />
              )}
            </div>
          </InfoPanel>
        </Section>
      )}

      {movies.length > 0 && (
        <Section>
          <Rail title="Movies" accent="purple">
            {movies.map((movie) =>
              movie ? (
                <PosterCard
                  key={movie.ids?.trakt ?? movie.ids?.slug}
                  title={movie.title}
                  meta={String(movie.year ?? '')}
                  image={movie.images?.poster}
                  rating={
                    movie.rating ? (movie.rating / 2).toFixed(1) : undefined
                  }
                  type="movie"
                  aspect="2:3"
                  onClick={() =>
                    navigate({
                      to: '/movies/$id',
                      params: {
                        id: String(movie.ids?.trakt ?? movie.ids?.slug),
                      },
                    })
                  }
                />
              ) : null
            )}
          </Rail>
        </Section>
      )}

      {shows.length > 0 && (
        <Section>
          <Rail title="TV Shows" accent="cyan">
            {shows.map((show) =>
              show ? (
                <PosterCard
                  key={show.ids?.trakt ?? show.ids?.slug}
                  title={show.title}
                  meta={String(show.year ?? '')}
                  image={show.images?.poster}
                  rating={
                    show.rating ? (show.rating / 2).toFixed(1) : undefined
                  }
                  type="series"
                  aspect="2:3"
                  onClick={() =>
                    navigate({
                      to: '/shows/$id',
                      params: { id: String(show.ids?.trakt ?? show.ids?.slug) },
                    })
                  }
                />
              ) : null
            )}
          </Rail>
        </Section>
      )}
    </Page>
  )
}
